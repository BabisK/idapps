// Package engine selects a local Docker engine without invoking the Docker CLI.
package engine

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BabisK/idapps/internal/config"
)

type Endpoint struct{ Context, Host string }

func SelectEndpoint() (Endpoint, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Endpoint{}, fmt.Errorf("resolve Docker configuration: %w", err)
	}
	settings := config.Environment()
	dir := settings.GetString("DOCKER_CONFIG")
	if dir == "" {
		dir = filepath.Join(home, ".docker")
	}
	context := settings.GetString("DOCKER_CONTEXT")
	host := settings.GetString("DOCKER_HOST")
	if context != "" {
		return findContext(dir, context)
	}
	if host != "" {
		return validateEndpoint(Endpoint{Host: host})
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err == nil {
		var cfg struct {
			CurrentContext string `json:"currentContext"`
		}
		if err = json.Unmarshal(b, &cfg); err != nil {
			return Endpoint{}, fmt.Errorf("invalid Docker config: %w", err)
		}
		if cfg.CurrentContext != "" && cfg.CurrentContext != "default" {
			return findContext(dir, cfg.CurrentContext)
		}
	} else if !os.IsNotExist(err) {
		return Endpoint{}, fmt.Errorf("read Docker config: %w", err)
	}
	return defaultEndpoint(), nil
}
func defaultEndpoint() Endpoint {
	if runtime.GOOS == "windows" {
		return Endpoint{Context: "default", Host: "npipe:////./pipe/docker_engine"}
	}
	return Endpoint{Context: "default", Host: "unix:///var/run/docker.sock"}
}
func findContext(dir, name string) (Endpoint, error) {
	if name == "default" {
		return defaultEndpoint(), nil
	}
	entries, err := os.ReadDir(filepath.Join(dir, "contexts", "meta"))
	if err != nil {
		return Endpoint{}, fmt.Errorf("Docker context %q unavailable: %w", name, err)
	}
	var found *Endpoint
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "contexts", "meta", entry.Name(), "meta.json"))
		if err != nil {
			return Endpoint{}, fmt.Errorf("read Docker context: %w", err)
		}
		var m struct {
			Name      string `json:"Name"`
			Endpoints map[string]struct {
				Host string `json:"Host"`
			} `json:"Endpoints"`
		}
		if err = json.Unmarshal(b, &m); err != nil {
			return Endpoint{}, fmt.Errorf("invalid Docker context: %w", err)
		}
		if m.Name == name {
			if found != nil {
				return Endpoint{}, fmt.Errorf("duplicate Docker context %q", name)
			}
			ep := Endpoint{Context: name, Host: m.Endpoints["docker"].Host}
			found = &ep
		}
	}
	if found == nil {
		return Endpoint{}, fmt.Errorf("Docker context %q not found", name)
	}
	return validateEndpoint(*found)
}
func validateEndpoint(e Endpoint) (Endpoint, error) {
	u, err := url.Parse(e.Host)
	if err != nil {
		return Endpoint{}, fmt.Errorf("invalid Docker endpoint for %q", e.Context)
	}
	switch u.Scheme {
	case "unix":
		if u.Host != "" || !strings.HasPrefix(u.Path, "/") || u.RawQuery != "" || u.Fragment != "" {
			break
		}
		return e, nil
	case "npipe":
		if strings.HasPrefix(strings.ToLower(e.Host), "npipe:////./pipe/") && len(e.Host) > len("npipe:////./pipe/") && !strings.ContainsAny(e.Host[len("npipe:////./pipe/"):], "/?#") {
			return e, nil
		}
	}
	return Endpoint{}, fmt.Errorf("Docker endpoint scheme %q for context %q is not a supported local socket or pipe; select a local Docker context", u.Scheme, e.Context)
}
