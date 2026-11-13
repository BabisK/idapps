// Package testutil provides fixtures for launcher tests.
package testutil

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// CatalogueFiles reads a catalogue tree into archive-relative paths.
func CatalogueFiles(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files["repo/catalogue/"+filepath.ToSlash(rel)] = data
		return nil
	})
	return files, err
}

// CatalogueArchive creates a repository-style ZIP from fixture files.
func CatalogueArchive(files map[string][]byte) ([]byte, error) {
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = f.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
