// Package compat checks a catalogue against parsers from supported release tags.
package compat

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BabisK/idapps/internal/catalogue"
	"golang.org/x/mod/semver"
)

// Check validates the current catalogue and the parser at every supported tag.
func Check(repo, cataloguePath string) error {
	data, err := os.ReadFile(cataloguePath)
	if err != nil {
		return err
	}
	var header struct {
		Minimum string `json:"minimumLauncherVersion"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	if !validTag("v" + header.Minimum) {
		return fmt.Errorf("invalid minimum launcher version %q", header.Minimum)
	}
	if _, err := catalogue.Parse(data, header.Minimum); err != nil {
		return fmt.Errorf("current parser: %w", err)
	}
	tags, err := git(repo, "tag", "--list")
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(cataloguePath)
	if err != nil {
		return err
	}
	for _, tag := range strings.Fields(string(tags)) {
		if !validTag(tag) || semver.Compare(tag, "v"+header.Minimum) < 0 {
			continue
		}
		if err := checkTag(repo, tag, absolute); err != nil {
			return fmt.Errorf("catalogue incompatible with %s: %w", tag, err)
		}
	}
	return nil
}

func validTag(tag string) bool {
	if !strings.HasPrefix(tag, "v") || !semver.IsValid(tag) {
		return false
	}
	core := strings.SplitN(strings.SplitN(tag[1:], "+", 2)[0], "-", 2)[0]
	return strings.Count(core, ".") == 2
}

func git(repo string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}

func checkTag(repo, tag, cataloguePath string) error {
	archive, err := git(repo, "archive", "--format=tar", tag)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "idapps-compat-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(filepath.FromSlash(h.Name))
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe archive path %q", h.Name)
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name != "go.mod" && name != "go.sum" && !strings.HasPrefix(name, "internal"+string(filepath.Separator)) {
			continue
		}
		file := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(f, reader)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	testFile := filepath.Join(dir, "internal", "catalogue", "idapps_compat_check_test.go")
	const harness = `package catalogue
import ("os"; "testing")
func TestIDAppsCompatibility(t *testing.T) {
 b, err := os.ReadFile(os.Getenv("IDAPPS_COMPAT_CATALOGUE")); if err != nil { t.Fatal(err) }
 if _, err = Parse(b, os.Getenv("IDAPPS_COMPAT_VERSION")); err != nil { t.Fatal(err) }
}`
	if err := os.WriteFile(testFile, []byte(harness), 0600); err != nil {
		return fmt.Errorf("release has no compatible catalogue parser: %w", err)
	}
	cmd := exec.Command("go", "test", "./internal/catalogue", "-run", "^TestIDAppsCompatibility$", "-count=1")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "IDAPPS_COMPAT_CATALOGUE="+cataloguePath, "IDAPPS_COMPAT_VERSION="+strings.TrimPrefix(tag, "v"), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("release parser failed: %w\n%s", err, out)
	}
	return nil
}
