// Package catalogue validates untrusted catalogue documents before use.
package catalogue

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/distribution/reference"
	"github.com/google/jsonschema-go/jsonschema"
	"golang.org/x/mod/semver"
)

const MaxBytes = 5 << 20

//go:embed schema.json
var schemaBytes []byte

type Catalogue struct {
	SchemaVersion          int            `json:"schemaVersion"`
	MinimumLauncherVersion string         `json:"minimumLauncherVersion"`
	Apps                   map[string]App `json:"apps"`
}
type Image struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest,omitempty"`
}

func (i Image) Reference() string {
	if i.Digest != "" {
		return i.Repository + "@" + i.Digest
	}
	return i.Repository + ":" + i.Tag
}

type User struct {
	Linux   string `json:"linux"`
	Darwin  string `json:"darwin"`
	Windows string `json:"windows"`
}
type Source struct {
	Base string `json:"base"`
	Path string `json:"path"`
}
type Mount struct {
	Type     string `json:"type"`
	Source   Source `json:"source"`
	Name     string `json:"name"`
	Target   string `json:"target"`
	Create   bool   `json:"create"`
	ReadOnly bool   `json:"readOnly"`
}
type Port struct {
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol"`
	HostIP        string `json:"hostIP"`
}
type App struct {
	Kind          string            `json:"kind"`
	Image         Image             `json:"image"`
	Entrypoint    []string          `json:"entrypoint"`
	Args          []string          `json:"args"`
	Workdir       string            `json:"workdir"`
	Environment   map[string]string `json:"environment"`
	Network       string            `json:"network"`
	User          User              `json:"user"`
	Mounts        []Mount           `json:"mounts"`
	ContainerName string            `json:"containerName"`
	Ports         []Port            `json:"ports"`
}

// Parse validates structure, duplicate keys, semantic constraints and version.
func Parse(data []byte, launcherVersion string) (*Catalogue, error) {
	if err := CheckJSON(data); err != nil {
		return nil, err
	}
	var doc any
	// Google's validator classifies json.Number as a string, so use the JSON
	// decoder's float64 representation here. The typed decode below still
	// rejects fractional or out-of-range integers before the catalogue is used.
	d := json.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&doc); err != nil {
		return nil, err
	}
	var schemaDoc jsonschema.Schema
	if err := json.Unmarshal(bytes.ReplaceAll(schemaBytes, []byte(`\\u0000`), []byte(`\\x00`)), &schemaDoc); err != nil {
		return nil, err
	}
	schema, err := schemaDoc.Resolve(nil)
	if err != nil {
		return nil, err
	}
	if err = schema.Validate(doc); err != nil {
		return nil, fmt.Errorf("invalid catalogue: %w", err)
	}
	var c Catalogue
	if err = json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if !validVersion(c.MinimumLauncherVersion) {
		return nil, fmt.Errorf("invalid minimum launcher version %q", c.MinimumLauncherVersion)
	}
	if !validVersion(launcherVersion) {
		return nil, fmt.Errorf("invalid launcher version %q", launcherVersion)
	}
	if semver.Compare("v"+launcherVersion, "v"+c.MinimumLauncherVersion) < 0 {
		return nil, fmt.Errorf("catalogue requires launcher %s or newer", c.MinimumLauncherVersion)
	}
	containers := map[string]string{}
	volumes := map[string]string{}
	for name, a := range c.Apps {
		parsed, err := reference.ParseNormalizedNamed(a.Image.Repository)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid image repository: %w", name, err)
		}
		if _, ok := parsed.(reference.Tagged); ok {
			return nil, fmt.Errorf("%s: repository contains a tag", name)
		}
		if _, ok := parsed.(reference.Digested); ok {
			return nil, fmt.Errorf("%s: repository contains a digest", name)
		}
		if a.Workdir != "" && !containerPath(a.Workdir) {
			return nil, fmt.Errorf("%s: invalid workdir", name)
		}
		if a.Kind == "service" {
			if prev := containers[a.ContainerName]; prev != "" {
				return nil, fmt.Errorf("%s and %s share container name", prev, name)
			}
			containers[a.ContainerName] = name
		}
		targets := map[string]bool{}
		for _, m := range a.Mounts {
			if !containerPath(m.Target) || targets[path.Clean(m.Target)] {
				return nil, fmt.Errorf("%s: invalid or duplicate mount target %q", name, m.Target)
			}
			targets[path.Clean(m.Target)] = true
			if m.Type == "volume" {
				if prev := volumes[m.Name]; prev != "" && prev != name {
					return nil, fmt.Errorf("%s and %s share volume %s", prev, name, m.Name)
				}
				volumes[m.Name] = name
			} else {
				p := m.Source.Path
				if strings.ContainsAny(p, `\:$~`) || strings.HasPrefix(p, "/") {
					return nil, fmt.Errorf("%s: invalid bind path", name)
				}
				if p != "" {
					for _, s := range strings.Split(p, "/") {
						if s == "" || s == "." || s == ".." {
							return nil, fmt.Errorf("%s: invalid bind path", name)
						}
					}
				}
				if m.Source.Base == "cwd" && m.Create {
					return nil, fmt.Errorf("%s: cwd cannot be created", name)
				}
			}
		}
		ports := map[string]bool{}
		for _, p := range a.Ports {
			key := fmt.Sprintf("%s/%d/%s", p.HostIP, p.HostPort, p.Protocol)
			if ports[key] {
				return nil, fmt.Errorf("%s: duplicate port binding", name)
			}
			ports[key] = true
		}
	}
	return &c, nil
}

// CheckJSON rejects oversized, non-UTF-8, duplicate-key and trailing documents.
func CheckJSON(data []byte) error {
	if len(data) > MaxBytes || !utf8.Valid(data) {
		return fmt.Errorf("JSON exceeds 5 MiB or is not UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := checkValue(dec); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON document")
	}
	return nil
}

func containerPath(p string) bool {
	if !strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if s == "." || s == ".." {
			return false
		}
	}
	return true
}

func validVersion(version string) bool {
	core := strings.SplitN(version, "+", 2)[0]
	core = strings.SplitN(core, "-", 2)[0]
	return strings.Count(core, ".") == 2 && semver.IsValid("v"+version)
}
func checkValue(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return err
			}
			s, ok := k.(string)
			if !ok {
				return fmt.Errorf("invalid object key")
			}
			if seen[s] {
				return fmt.Errorf("duplicate key %q", s)
			}
			seen[s] = true
			if err = checkValue(d); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	case '[':
		for d.More() {
			if err = checkValue(d); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
}

// ValidateServiceDefinition validates a durable service snapshot independently
// of the current catalogue, which may be absent or no longer contain the app.
func ValidateServiceDefinition(name string, app App) error {
	image := map[string]any{"repository": app.Image.Repository, "tag": app.Image.Tag}
	if app.Image.Digest != "" {
		image["digest"] = app.Image.Digest
	}
	definition := map[string]any{"kind": app.Kind, "image": image, "network": app.Network, "user": app.User, "containerName": app.ContainerName, "ports": app.Ports}
	if app.Entrypoint != nil {
		definition["entrypoint"] = app.Entrypoint
	}
	if app.Args != nil {
		definition["args"] = app.Args
	}
	if app.Workdir != "" {
		definition["workdir"] = app.Workdir
	}
	if app.Environment != nil {
		definition["environment"] = app.Environment
	}
	if app.Mounts != nil {
		mounts := make([]map[string]any, 0, len(app.Mounts))
		for _, m := range app.Mounts {
			item := map[string]any{"type": m.Type, "target": m.Target, "readOnly": m.ReadOnly}
			if m.Type == "volume" {
				item["name"] = m.Name
			} else {
				item["source"] = m.Source
				item["create"] = m.Create
			}
			mounts = append(mounts, item)
		}
		definition["mounts"] = mounts
	}
	data, err := json.Marshal(map[string]any{"schemaVersion": 1, "minimumLauncherVersion": "0.0.0", "apps": map[string]any{name: definition}})
	if err != nil {
		return err
	}
	_, err = Parse(data, "0.0.0")
	return err
}
