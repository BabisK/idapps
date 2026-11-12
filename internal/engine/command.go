package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/BabisK/idapps/internal/catalogue"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/versions"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"
	"golang.org/x/term"
)

func connect(ctx context.Context) (*client.Client, error) {
	endpoint, e := SelectEndpoint()
	if e != nil {
		return nil, e
	}
	cli, e := client.NewClientWithOpts(client.WithHost(endpoint.Host), client.WithAPIVersionNegotiation())
	if e != nil {
		return nil, e
	}
	if e = checkEngine(ctx, cli, endpoint); e != nil {
		cli.Close()
		return nil, e
	}
	return cli, nil
}
func checkEngine(ctx context.Context, cli *client.Client, endpoint Endpoint) error {
	info, e := cli.Info(ctx)
	if e != nil {
		return fmt.Errorf("Docker engine %s unavailable: %w; start Docker and check permissions", endpoint.Host, e)
	}
	if versions.LessThan(cli.ClientVersion(), "1.30") {
		return fmt.Errorf("Docker API %s is too old; API 1.30 or newer is required", cli.ClientVersion())
	}
	if info.OSType != "linux" {
		return fmt.Errorf("Docker engine %s is not using Linux containers", endpoint.Host)
	}
	return nil
}
func ensureImage(ctx context.Context, cli *client.Client, ref string) error {
	_, e := cli.ImageInspect(ctx, ref)
	if e == nil {
		return nil
	}
	if !errdefs.IsNotFound(e) {
		return fmt.Errorf("inspect image %s: %w", ref, e)
	}
	r, e := cli.ImagePull(ctx, ref, image.PullOptions{})
	if e != nil {
		return fmt.Errorf("pull image %s anonymously: %w", ref, e)
	}
	defer r.Close()
	return reportPullProgress(r, os.Stderr, ref)
}

func reportPullProgress(r io.Reader, output io.Writer, ref string) error {
	d := json.NewDecoder(r)
	statuses := make(map[string]string)
	for {
		var line struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
		}
		e := d.Decode(&line)
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return fmt.Errorf("read image pull: %w", e)
		}
		if line.Error != "" || line.ErrorDetail.Message != "" {
			return fmt.Errorf("pull image %s: %s %s", ref, line.Error, line.ErrorDetail.Message)
		}
		if line.Status != "" && statuses[line.ID] != line.Status {
			statuses[line.ID] = line.Status
			if line.ID == "" {
				fmt.Fprintln(output, line.Status)
			} else {
				fmt.Fprintf(output, "%s: %s\n", line.ID, line.Status)
			}
		}
	}
}
func bindSources(a catalogue.App) ([]mount.Mount, error) {
	var binds []mount.Mount
	for _, m := range a.Mounts {
		if m.Type != "bind" {
			return nil, fmt.Errorf("unexpected volume in command")
		}
		var base string
		var e error
		if m.Source.Base == "home" {
			base, e = os.UserHomeDir()
		} else {
			base, e = os.Getwd()
		}
		if e != nil {
			return nil, e
		}
		src := filepath.Join(base, filepath.FromSlash(m.Source.Path))
		if m.Create {
			if e = os.MkdirAll(src, 0700); e != nil {
				return nil, e
			}
		}
		info, e := os.Stat(src)
		if e != nil || !info.IsDir() {
			return nil, fmt.Errorf("bind source %s is not an accessible directory", src)
		}
		resolved, e := filepath.EvalSymlinks(src)
		if e != nil {
			return nil, e
		}
		if strings.Contains(strings.ToLower(filepath.Base(resolved)), "docker.sock") {
			return nil, fmt.Errorf("Docker socket mounts are forbidden")
		}
		binds = append(binds, mount.Mount{Type: mount.TypeBind, Source: src, Target: m.Target, ReadOnly: m.ReadOnly})
	}
	return binds, nil
}

// terminalStreams reports whether Docker may combine streams in TTY mode.
func terminalStreams(stdin, stdout, stderr *os.File) bool {
	return term.IsTerminal(int(stdin.Fd())) && term.IsTerminal(int(stdout.Fd())) && term.IsTerminal(int(stderr.Fd()))
}

// commandFailure keeps cancellation distinct from launcher and engine errors.
func commandFailure(ctx context.Context, err error) (int, error) {
	if ctx.Err() != nil {
		return 130, ctx.Err()
	}
	return 125, err
}

// removeCommandContainer tolerates Docker's concurrent automatic removal.
func removeCommandContainer(ctx context.Context, cli *client.Client, id string) error {
	for {
		err := cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
		if err == nil || errdefs.IsNotFound(err) {
			return nil
		}
		if !errdefs.IsConflict(err) || !strings.Contains(strings.ToLower(err.Error()), "removal of container") || !strings.Contains(strings.ToLower(err.Error()), "is already in progress") {
			return err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("container removal still in progress: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// RunCommand runs one ephemeral command, preserving native argv and non-TTY streams.
func RunCommand(ctx context.Context, a catalogue.App, args []string) (int, error) {
	stdin, stdout, stderr := os.Stdin, os.Stdout, os.Stderr
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	cli, e := openEngine(ctx)
	if e != nil {
		return commandFailure(ctx, e)
	}
	defer cli.Close()
	if e = ensureImage(ctx, cli, a.Image.Reference()); e != nil {
		return commandFailure(ctx, e)
	}
	binds, e := bindSources(a)
	if e != nil {
		return commandFailure(ctx, e)
	}
	tty := terminalStreams(stdin, stdout, stderr)
	cfg := &container.Config{Image: a.Image.Reference(), Entrypoint: a.Entrypoint, WorkingDir: a.Workdir, OpenStdin: true, StdinOnce: true, Tty: tty, AttachStdin: true, AttachStdout: true, AttachStderr: true}
	if a.Args != nil {
		cfg.Cmd = append(append([]string{}, a.Args...), args...)
	} else if len(args) > 0 {
		cfg.Cmd = args
	}
	for k, v := range a.Environment {
		cfg.Env = append(cfg.Env, k+"="+v)
	}
	if runtime.GOOS != "windows" && ((runtime.GOOS == "linux" && a.User.Linux == "host") || (runtime.GOOS == "darwin" && a.User.Darwin == "host")) {
		cfg.User = fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	}
	host := &container.HostConfig{Mounts: binds, NetworkMode: container.NetworkMode(a.Network), AutoRemove: true}
	if tty {
		// Windows console size queries require an output handle; stdin can be a console without a screen buffer.
		width, height, err := term.GetSize(int(stdout.Fd()))
		if err != nil {
			return commandFailure(ctx, fmt.Errorf("read terminal size: %w", err))
		}
		host.ConsoleSize = [2]uint{uint(height), uint(width)}
	}
	created, e := cli.ContainerCreate(ctx, cfg, host, nil, nil, "")
	if e != nil {
		return commandFailure(ctx, fmt.Errorf("create command container: %w", e))
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := removeCommandContainer(cleanupCtx, cli, created.ID); err != nil {
			fmt.Fprintf(stderr, "could not remove command container %s: %v\n", created.ID, err)
		}
	}()
	// A separate stdin attachment allows EOF to close its connection on local
	// named pipes, which do not support half-close, while output continues.
	attached, e := cli.ContainerAttach(ctx, created.ID, container.AttachOptions{Stream: true, Stdin: tty, Stdout: true, Stderr: true})
	if e != nil {
		return commandFailure(ctx, e)
	}
	defer attached.Close()
	input := attached
	if !tty {
		input, e = cli.ContainerAttach(ctx, created.ID, container.AttachOptions{Stream: true, Stdin: true})
		if e != nil {
			return commandFailure(ctx, e)
		}
		defer input.Close()
	}
	if tty {
		state, err := term.MakeRaw(int(stdin.Fd()))
		if err != nil {
			return commandFailure(ctx, err)
		}
		defer term.Restore(int(stdin.Fd()), state)
	}
	wait, errs := cli.ContainerWait(ctx, created.ID, container.WaitConditionNextExit)
	interrupt := func() {
		interruptCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = cli.ContainerKill(interruptCtx, created.ID, "SIGINT")
	}
	if e = cli.ContainerStart(ctx, created.ID, container.StartOptions{}); e != nil {
		if ctx.Err() != nil {
			interrupt()
		}
		return commandFailure(ctx, fmt.Errorf("start command container: %w", e))
	}
	streamFailure := func(err error) (int, error) {
		if ctx.Err() != nil {
			interrupt()
		}
		return commandFailure(ctx, err)
	}
	if tty {
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			lastW, lastH := 0, 0
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					w, h, err := term.GetSize(int(stdout.Fd()))
					if err == nil && (w != lastW || h != lastH) {
						_ = cli.ContainerResize(ctx, created.ID, container.ResizeOptions{Width: uint(w), Height: uint(h)})
						lastW, lastH = w, h
					}
				}
			}
		}()
	}
	go func() {
		_, _ = io.Copy(input.Conn, stdin)
		if tty {
			_ = input.CloseWrite()
		} else {
			input.Close()
		}
	}()
	out := make(chan error, 1)
	go func() {
		var err error
		if tty {
			_, err = io.Copy(stdout, attached.Reader)
		} else {
			_, err = stdcopy.StdCopy(stdout, stderr, attached.Reader)
		}
		out <- err
	}()
	var code *int
	var outputDone bool
	for {
		select {
		case result := <-wait:
			if result.Error != nil {
				return streamFailure(fmt.Errorf("container wait: %s", result.Error.Message))
			}
			v := int(result.StatusCode)
			code = &v
			wait = nil
		case err := <-errs:
			if err != nil {
				return streamFailure(err)
			}
			errs = nil
		case err := <-out:
			if err != nil {
				return streamFailure(err)
			}
			outputDone = true
			out = nil
		case <-ctx.Done():
			interrupt()
			return 130, ctx.Err()
		}
		if code != nil && outputDone {
			return *code, nil
		}
		if wait == nil && errs == nil && code == nil {
			return commandFailure(ctx, fmt.Errorf("container wait ended without status"))
		}
	}
}
