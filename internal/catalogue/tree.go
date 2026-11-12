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

// Assemble builds the legacy aggregate from metadata.json and app descriptors.
// Each input is checked before decoding, so duplicate keys cannot be discarded.
func Assemble(files map[string][]byte, version string) ([]byte, error) {
	metadata, ok := files["metadata.json"]
	if !ok {
		return nil, fmt.Errorf("missing catalogue metadata")
	}
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
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &meta); err != nil || meta == nil {
		return nil, fmt.Errorf("invalid catalogue metadata")
	}
	if len(meta) != 2 || meta["schemaVersion"] == nil || meta["minimumLauncherVersion"] == nil {
		return nil, fmt.Errorf("invalid catalogue metadata fields")
	}
	apps := make(map[string]json.RawMessage)
	for name, data := range files {
		if name == "metadata.json" || name == "catalogue.schema.json" {
			continue
		}
		parts := strings.Split(name, "/")
		if len(parts) != 2 || !appName.MatchString(parts[0]) || parts[1] != "app.json" {
			return nil, fmt.Errorf("invalid descriptor path %q", name)
		}
		if _, exists := apps[parts[0]]; exists {
			return nil, fmt.Errorf("duplicate app %q", parts[0])
		}
		apps[parts[0]] = data
	}
	if len(apps) == 0 {
		return nil, fmt.Errorf("catalogue has no descriptors")
	}
	aggregate := struct {
		Schema                 string                     `json:"$schema"`
		SchemaVersion          json.RawMessage            `json:"schemaVersion"`
		MinimumLauncherVersion json.RawMessage            `json:"minimumLauncherVersion"`
		Apps                   map[string]json.RawMessage `json:"apps"`
	}{"", meta["schemaVersion"], meta["minimumLauncherVersion"], apps}
	data, err := json.MarshalIndent(aggregate, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if _, err = Parse(data, version); err != nil {
		return nil, err
	}
	return data, nil
}

// ReadTree reads only regular files and rejects unexpected entries or symlinks.
func ReadTree(root string, version string) ([]byte, error) {
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
	return Assemble(files, version)
}
