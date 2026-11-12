// Package version derives a launcher's semantic version from Go build metadata.
package version

import (
	"runtime/debug"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// Current reports the embedded version, falling back to Go's build metadata.
// Official build tooling may inject a version when Go's module metadata cannot
// identify the correct release tag.
func Current(embedded string) string {
	if embedded != "" {
		return embedded
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "0.0.0+unknown"
	}
	return FromBuildInfo(info)
}

// FromBuildInfo derives a version without requiring Git at runtime.
func FromBuildInfo(info *debug.BuildInfo) string {
	if info == nil {
		return "0.0.0+unknown"
	}
	var revision string
	var dirty bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	moduleVersion := info.Main.Version
	if strings.HasSuffix(moduleVersion, "+incompatible.dirty") {
		moduleVersion = strings.TrimSuffix(moduleVersion, ".dirty")
		dirty = true
	} else if strings.HasSuffix(moduleVersion, "+dirty") {
		moduleVersion = strings.TrimSuffix(moduleVersion, "+dirty")
		dirty = true
	}
	base := moduleVersion
	development := module.IsPseudoVersion(moduleVersion)
	if development {
		if revision == "" {
			revision, _ = module.PseudoVersionRev(moduleVersion)
		}
		var err error
		base, err = module.PseudoVersionBase(moduleVersion)
		if err != nil {
			base = ""
		}
	}
	// +incompatible is Go module metadata, not part of the release tag.
	base = strings.TrimSuffix(base, "+incompatible")
	if !semver.IsValid(base) || base == "" {
		base = "v0.0.0"
		development = true
	}
	base = strings.TrimPrefix(base, "v")
	if development || dirty {
		if revision == "" {
			revision = "unknown"
		}
		separator := "+"
		if strings.Contains(base, "+") {
			separator = "."
		}
		base += separator + revision
		if dirty {
			base += ".dirty"
		}
	}
	return base
}
