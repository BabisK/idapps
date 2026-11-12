package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BabisK/idapps/internal/catalogue"
	"github.com/BabisK/idapps/internal/update"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

var openEngine = connect

// ErrUsage identifies invalid service commands and options.
var ErrUsage = errors.New("service usage error")

const ownerLabel = "io.idapps.installation"
const appLabel = "io.idapps.app"

func owned(labels map[string]string, id, name string) bool {
	return labels[ownerLabel] == id && labels[appLabel] == name
}
func serviceVolumes(a catalogue.App) []string {
	var out []string
	for _, m := range a.Mounts {
		if m.Type == "volume" {
			out = append(out, m.Name)
		}
	}
	return out
}

// Service performs lifecycle work while the store lock is held by the caller.
func Service(ctx context.Context, s *update.Store, name string, current *catalogue.App, args []string) error {
	verb := "start"
	rest := args
	if len(args) > 0 {
		verb = args[0]
		rest = args[1:]
	}
	if verb != "start" && verb != "stop" && verb != "clean" {
		return fmt.Errorf("%w: unknown service operation %q", ErrUsage, verb)
	}
	if verb != "logs" && len(rest) > 0 {
		return fmt.Errorf("%w: %s takes no arguments", ErrUsage, verb)
	}
	id, records, e := s.Resources()
	if e != nil {
		return e
	}
	rec, recorded := records[name]
	if verb == "start" && current == nil {
		return fmt.Errorf("service %s is retired; it cannot start", name)
	}
	if verb != "start" && !recorded {
		return fmt.Errorf("no owned resources recorded for %s", name)
	}
	app := rec.Definition
	if verb == "start" {
		app = *current
	}
	if verb == "clean" && len(serviceVolumes(app)) == 0 {
		return fmt.Errorf("service %s has no clean operation", name)
	}
	cli, e := openEngine(ctx)
	if e != nil {
		return e
	}
	defer cli.Close()
	info, e := cli.Info(ctx)
	if e != nil {
		return e
	}
	if info.ID == "" {
		return fmt.Errorf("Docker engine has no stable identity")
	}
	if recorded && rec.Engine != info.ID {
		return fmt.Errorf("%s belongs to a different Docker engine", name)
	}
	labels := map[string]string{ownerLabel: id.Installation, appLabel: name}
	inspect, e := cli.ContainerInspect(ctx, app.ContainerName)
	if e != nil && !errdefs.IsNotFound(e) {
		return e
	}
	exists := e == nil
	if exists && (inspect.Config == nil || !owned(inspect.Config.Labels, id.Installation, name)) {
		return fmt.Errorf("container %s is not owned by this installation", app.ContainerName)
	}
	if exists && (inspect.ID == "" || inspect.State == nil) {
		return fmt.Errorf("Docker returned incomplete state for container %s", app.ContainerName)
	}
	if exists && recorded && rec.ContainerID != "" && inspect.ID != rec.ContainerID {
		return fmt.Errorf("recorded container identity does not match %s", app.ContainerName)
	}
	if verb == "start" {
		if exists && inspect.State.Running {
			return fmt.Errorf("service %s is already running", name)
		}
		missingVolumes := make(map[string]bool)
		for _, n := range serviceVolumes(app) {
			v, err := cli.VolumeInspect(ctx, n)
			if errdefs.IsNotFound(err) {
				missingVolumes[n] = true
				continue
			}
			if err != nil {
				return err
			}
			if !owned(v.Labels, id.Installation, name) {
				return fmt.Errorf("volume %s is not owned by this installation", n)
			}
		}
		if exists {
			if e = cli.ContainerRemove(ctx, inspect.ID, container.RemoveOptions{}); e != nil {
				return e
			}
		}
		if e = ensureImage(ctx, cli, app.Image.Reference()); e != nil {
			return e
		}
		rec = update.ServiceRecord{App: name, Engine: info.ID, Definition: app}
		records[name] = rec
		if e = s.SaveResources(id, records); e != nil {
			return e
		}
		for _, n := range serviceVolumes(app) {
			if missingVolumes[n] {
				v, err := cli.VolumeCreate(ctx, volume.CreateOptions{Name: n, Labels: labels})
				if err != nil {
					return err
				}
				if !owned(v.Labels, id.Installation, name) {
					return fmt.Errorf("volume %s is not owned by this installation", n)
				}
			}
		}
		cfg := &container.Config{Image: app.Image.Reference(), Entrypoint: app.Entrypoint, Cmd: app.Args, WorkingDir: app.Workdir, Labels: labels, ExposedPorts: nat.PortSet{}}
		for k, v := range app.Environment {
			cfg.Env = append(cfg.Env, k+"="+v)
		}
		hc := &container.HostConfig{AutoRemove: true, NetworkMode: container.NetworkMode(app.Network), PortBindings: nat.PortMap{}}
		for _, p := range app.Ports {
			port := nat.Port(fmt.Sprintf("%d/%s", p.ContainerPort, p.Protocol))
			cfg.ExposedPorts[port] = struct{}{}
			hc.PortBindings[port] = append(hc.PortBindings[port], nat.PortBinding{HostIP: p.HostIP, HostPort: strconv.Itoa(p.HostPort)})
		}
		bindApp := app
		bindApp.Mounts = nil
		for _, m := range app.Mounts {
			if m.Type == "volume" {
				hc.Mounts = append(hc.Mounts, mount.Mount{Type: mount.TypeVolume, Source: m.Name, Target: m.Target, ReadOnly: m.ReadOnly})
			} else {
				bindApp.Mounts = append(bindApp.Mounts, m)
			}
		}
		binds, err := bindSources(bindApp)
		if err != nil {
			return err
		}
		hc.Mounts = append(hc.Mounts, binds...)
		created, err := cli.ContainerCreate(ctx, cfg, hc, nil, nil, app.ContainerName)
		if err != nil {
			return err
		}
		rec.ContainerID = created.ID
		records[name] = rec
		if err = s.SaveResources(id, records); err != nil {
			return err
		}
		if err = cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
			return err
		}
		for _, p := range app.Ports {
			fmt.Fprintf(os.Stdout, "%s:%d -> %d/%s\n", p.HostIP, p.HostPort, p.ContainerPort, p.Protocol)
		}
		return nil
	}
	switch verb {
	case "stop":
		if !exists {
			if len(serviceVolumes(app)) == 0 {
				delete(records, name)
				return s.SaveResources(id, records)
			}
			return fmt.Errorf("service %s has no container", name)
		}
		if inspect.State.Running {
			if e = cli.ContainerStop(ctx, inspect.ID, container.StopOptions{}); e != nil {
				return e
			}
		} else if e = cli.ContainerRemove(ctx, inspect.ID, container.RemoveOptions{}); e != nil {
			return e
		}
		if len(serviceVolumes(app)) == 0 {
			delete(records, name)
			return s.SaveResources(id, records)
		}
		return nil
	case "clean":
		if exists && inspect.State.Running {
			return fmt.Errorf("service %s is running", name)
		}
		containers, e := cli.ContainerList(ctx, container.ListOptions{All: true})
		if e != nil {
			return e
		}
		names := serviceVolumes(app)
		var foreignVolume string
		ownedVolume := false
		for _, n := range names {
			v, e := cli.VolumeInspect(ctx, n)
			if errdefs.IsNotFound(e) {
				continue
			}
			if e != nil {
				return e
			}
			if !owned(v.Labels, id.Installation, name) {
				foreignVolume = n
				continue
			}
			ownedVolume = true
			for _, c := range containers {
				for _, m := range c.Mounts {
					if m.Name == n {
						return fmt.Errorf("volume %s is in use by container %s", n, c.ID)
					}
				}
			}
		}
		if foreignVolume != "" {
			if !exists && !ownedVolume {
				delete(records, name)
				if err := s.SaveResources(id, records); err != nil {
					return err
				}
				return fmt.Errorf("volume %s is not owned; stale service record removed", foreignVolume)
			}
			return fmt.Errorf("volume %s is not owned", foreignVolume)
		}
		for _, n := range names {
			if e = cli.VolumeRemove(ctx, n, false); e != nil && !errdefs.IsNotFound(e) {
				return fmt.Errorf("remove volume %s: %w", n, e)
			}
		}
		if !exists {
			delete(records, name)
		}
		return s.SaveResources(id, records)
	}
	return nil
}

// ServiceLogs snapshots ownership under the update lock, then streams by exact
// container ID without holding that lock. A concurrent stop may end the stream.
func ServiceLogs(ctx context.Context, s *update.Store, name string, args []string) error {
	follow, tail, e := logOptions(args)
	if e != nil {
		return e
	}
	var id update.Identity
	var rec update.ServiceRecord
	e = s.WithLock(ctx, func() error {
		var records map[string]update.ServiceRecord
		var err error
		id, records, err = s.Resources()
		if err != nil {
			return err
		}
		var ok bool
		rec, ok = records[name]
		if !ok {
			return fmt.Errorf("no owned resources recorded for %s", name)
		}
		return nil
	})
	if e != nil {
		return e
	}
	cli, e := openEngine(ctx)
	if e != nil {
		return e
	}
	defer cli.Close()
	info, e := cli.Info(ctx)
	if e != nil {
		return e
	}
	if info.ID == "" {
		return fmt.Errorf("Docker engine has no stable identity")
	}
	if info.ID != rec.Engine {
		return fmt.Errorf("%s belongs to a different Docker engine", name)
	}
	inspect, e := cli.ContainerInspect(ctx, rec.Definition.ContainerName)
	if errdefs.IsNotFound(e) {
		return fmt.Errorf("service %s is not running", name)
	}
	if e != nil {
		return e
	}
	if inspect.Config == nil || !owned(inspect.Config.Labels, id.Installation, name) {
		return fmt.Errorf("container %s is not owned by this installation", rec.Definition.ContainerName)
	}
	if rec.ContainerID != "" && inspect.ID != rec.ContainerID {
		return fmt.Errorf("recorded container identity does not match %s", rec.Definition.ContainerName)
	}
	if inspect.State == nil || !inspect.State.Running {
		return fmt.Errorf("service %s is not running", name)
	}
	r, e := cli.ContainerLogs(ctx, inspect.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true, Follow: follow, Tail: tail})
	if e != nil {
		return e
	}
	defer r.Close()
	_, e = stdcopy.StdCopy(os.Stdout, os.Stderr, r)
	return e
}

func logOptions(args []string) (bool, string, error) {
	follow := false
	tail := "all"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--":
			if i != len(args)-1 {
				return false, "", fmt.Errorf("%w: unexpected log operands", ErrUsage)
			}
			return follow, tail, nil
		case "-f":
			follow = true
		case "-t":
			i++
			if i == len(args) {
				return false, "", fmt.Errorf("%w: -t requires a nonnegative integer", ErrUsage)
			}
			v, e := strconv.ParseUint(args[i], 10, 64)
			if e != nil {
				return false, "", fmt.Errorf("%w: invalid log tail %q", ErrUsage, args[i])
			}
			tail = strconv.FormatUint(v, 10)
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "warning: unknown logs option %s\n", args[i])
			} else {
				return false, "", fmt.Errorf("%w: unexpected log operand %q", ErrUsage, args[i])
			}
		}
	}
	return follow, tail, nil
}
