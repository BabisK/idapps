// Package storage resolves the per-user launcher directories.
package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BabisK/idapps/internal/config"
)

type Paths struct{ Cache, Config, Bin string }

// Resolve determines locations without creating directories or following links.
func Resolve() (Paths, error) {
	return resolve(runtime.GOOS, config.Environment().GetString, os.UserHomeDir)
}

func resolve(goos string, getenv func(string) string, homeDir func() (string, error)) (Paths, error) {
	home, err := homeDir()
	if err != nil || home == "" || !filepath.IsAbs(home) {
		return Paths{}, fmt.Errorf("cannot resolve absolute home directory")
	}
	get := func(key, fallback string) (string, error) {
		v := getenv(key)
		if v == "" {
			v = fallback
		}
		if !filepath.IsAbs(v) {
			return "", fmt.Errorf("%s must be absolute", key)
		}
		return v, nil
	}
	var cache, config, data string
	switch goos {
	case "linux":
		if cache, err = get("XDG_CACHE_HOME", filepath.Join(home, ".cache")); err != nil {
			return Paths{}, err
		}
		if config, err = get("XDG_CONFIG_HOME", filepath.Join(home, ".config")); err != nil {
			return Paths{}, err
		}
		if data, err = get("XDG_DATA_HOME", filepath.Join(home, ".local", "share")); err != nil {
			return Paths{}, err
		}
		return Paths{filepath.Join(cache, "idapps", "catalogues"), filepath.Join(config, "idapps"), filepath.Join(data, "idapps", "bin")}, nil
	case "darwin":
		return Paths{filepath.Join(home, "Library", "Caches", "idapps", "catalogues"), filepath.Join(home, "Library", "Application Support", "idapps"), filepath.Join(home, "Library", "Application Support", "idapps", "bin")}, nil
	case "windows":
		cache, err = get("LOCALAPPDATA", "")
		if err != nil {
			return Paths{}, err
		}
		config, err = get("APPDATA", "")
		if err != nil {
			return Paths{}, err
		}
		return Paths{filepath.Join(cache, "idapps", "cache", "catalogues"), filepath.Join(config, "idapps"), filepath.Join(cache, "idapps", "bin")}, nil
	default:
		return Paths{}, fmt.Errorf("unsupported operating system %s", goos)
	}
}
