package catalogue

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var appName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

func validAppName(name string) bool {
	if !appName.MatchString(name) {
		return false
	}
	switch name {
	case "idapps", "update", "run", "list", "version", "help", "con", "prn", "aux", "nul", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9", "lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		return false
	}
	return true
}

// ParseTree validates individual descriptors without constructing an aggregate document.
func ParseTree(files map[string][]byte, version string) (*Catalogue, error) {
	meta, ok := files["metadata.json"]
	if !ok {
		return nil, fmt.Errorf("missing catalogue metadata")
	}
	root := map[string]any{}
	total := 0
	for name, data := range files {
		total += len(data)
		if total > MaxBytes {
			return nil, fmt.Errorf("catalogue exceeds size limit")
		}
		if err := CheckJSON(data); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := json.Unmarshal(meta, &root); err != nil || root == nil || len(root) != 2 || root["schemaVersion"] == nil || root["minimumLauncherVersion"] == nil {
		return nil, fmt.Errorf("invalid catalogue metadata fields")
	}
	var typedMeta struct {
		SchemaVersion          int    `json:"schemaVersion"`
		MinimumLauncherVersion string `json:"minimumLauncherVersion"`
	}
	if err := json.Unmarshal(meta, &typedMeta); err != nil {
		return nil, fmt.Errorf("invalid catalogue metadata: %w", err)
	}
	apps := map[string]any{}
	for name, data := range files {
		if name == "metadata.json" || name == "catalogue.schema.json" || name == "schema_embed.go" {
			continue
		}
		parts := strings.Split(name, "/")
		if len(parts) != 2 || !appName.MatchString(parts[0]) || parts[1] != "app.json" {
			return nil, fmt.Errorf("invalid descriptor path %q", name)
		}
		var app any
		if err := json.Unmarshal(data, &app); err != nil {
			return nil, err
		}
		apps[parts[0]] = app
	}
	if len(apps) == 0 {
		return nil, fmt.Errorf("catalogue has no descriptors")
	}
	c, err := validateApps(apps, typedMeta.SchemaVersion, typedMeta.MinimumLauncherVersion, version)
	if err != nil {
		return nil, err
	}
	for name := range apps {
		var app App
		if err := json.Unmarshal(files[name+"/app.json"], &app); err != nil {
			return nil, err
		}
		c.Apps[name] = app
	}
	return c, nil
}

// ReadTree reads only regular files and rejects unexpected entries or symlinks.
func ReadTree(root string, version string) (*Catalogue, error) {
	files := map[string][]byte{}
	total := int64(0)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if strings.Contains(rel, "/") || !appName.MatchString(rel) {
				return fmt.Errorf("invalid directory %q", rel)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular catalogue entry %q", rel)
		}
		if rel == "schema_embed.go" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > MaxBytes {
			return fmt.Errorf("descriptor exceeds size limit")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[rel] = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Reject empty app directories, too.
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		if _, ok := files[d+"/app.json"]; !ok {
			return nil, fmt.Errorf("missing descriptor for %s", d)
		}
	}
	return ParseTree(files, version)
}
