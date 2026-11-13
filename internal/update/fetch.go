// Package update fetches and validates a pinned catalogue generation.
package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/BabisK/idapps/internal/catalogue"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

const maxArchiveBytes = 64 << 20
const maxExpandedBytes = 256 << 20

type Generation struct {
	Commit, SHA256 string
	Bytes          []byte
	Catalogue      *catalogue.Catalogue
}

func Fetch(ctx context.Context, version string) (*Generation, error) {
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: checkRedirect}
	return fetch(ctx, client, version, "https://api.github.com/repos/BabisK/idapps/commits/main", "https://codeload.github.com/BabisK/idapps/zip/")
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 || req.URL.Scheme != "https" || (req.URL.Host != "api.github.com" && req.URL.Host != "codeload.github.com") {
		return fmt.Errorf("refusing catalogue redirect to untrusted origin")
	}
	return nil
}

func fetch(ctx context.Context, client *http.Client, version, api, archive string) (*Generation, error) {
	get := func(url string, limit int64) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "idapps/"+version)
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: HTTP %s", url, res.Status)
		}
		b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
		if err != nil {
			return nil, err
		}
		if int64(len(b)) > limit {
			return nil, fmt.Errorf("response exceeds size limit")
		}
		return b, nil
	}
	b, err := get(api, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("resolve main: %w", err)
	}
	var head struct {
		SHA string `json:"sha"`
	}
	if catalogue.CheckJSON(b) != nil || json.Unmarshal(b, &head) != nil || !commitPattern.MatchString(head.SHA) {
		return nil, fmt.Errorf("invalid main commit response")
	}
	b, err = get(strings.TrimRight(archive, "/")+"/"+head.SHA, maxArchiveBytes)
	if err != nil {
		return nil, fmt.Errorf("download archive: %w", err)
	}
	c, err := parseArchive(b, version)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	return &Generation{head.SHA, hex.EncodeToString(sum[:]), b, c}, nil
}

// parseArchive validates paths and entry types globally, and reads only catalogue files.
func parseArchive(data []byte, version string) (*catalogue.Catalogue, error) {
	return parseArchiveMode(data, version, true)
}

func parseArchiveMode(data []byte, version string, verifyUnrelated bool) (*catalogue.Catalogue, error) {
	if len(data) > maxArchiveBytes {
		return nil, fmt.Errorf("archive exceeds size limit")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid archive: %w", err)
	}
	seen := map[string]bool{}
	regular := map[string]bool{}
	files := map[string][]byte{}
	dirs := map[string]bool{}
	root := ""
	var expanded uint64
	for _, f := range z.File {
		n := f.Name
		if n == "" || strings.ContainsAny(n, "\\:\x00") || strings.HasPrefix(n, "/") || path.Clean(n) != strings.TrimSuffix(n, "/") {
			return nil, fmt.Errorf("unsafe archive path %q", n)
		}
		key := strings.TrimSuffix(n, "/")
		if seen[key] {
			return nil, fmt.Errorf("duplicate archive path %q", n)
		}
		seen[key] = true
		if !f.FileInfo().IsDir() {
			regular[key] = true
		}
		parts := strings.Split(key, "/")
		if root == "" {
			root = parts[0]
		}
		if root != parts[0] || root == "." || root == ".." {
			return nil, fmt.Errorf("ambiguous archive root")
		}
		if f.Mode()&^(0777|1<<31) != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) {
			return nil, fmt.Errorf("unsafe archive entry %q", n)
		}
		if f.FileInfo().IsDir() && (f.UncompressedSize64 != 0 || f.CompressedSize64 != 0) {
			return nil, fmt.Errorf("directory has data %q", n)
		}
		if f.UncompressedSize64 > maxExpandedBytes-expanded {
			return nil, fmt.Errorf("archive expanded size limit exceeded")
		}
		expanded += f.UncompressedSize64
		if expanded > maxExpandedBytes {
			return nil, fmt.Errorf("archive expanded size limit exceeded")
		}
		if verifyUnrelated && !f.FileInfo().IsDir() && (len(parts) < 2 || parts[1] != "catalogue" || (len(parts) == 3 && parts[2] == "schema_embed.go")) {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			_, err = io.Copy(io.Discard, io.LimitReader(r, int64(f.UncompressedSize64)+1))
			closeErr := r.Close()
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
		if len(parts) < 2 || parts[1] != "catalogue" {
			continue
		}
		if len(parts) == 2 {
			if !f.FileInfo().IsDir() {
				return nil, fmt.Errorf("invalid catalogue directory")
			}
			continue
		}
		rel := strings.Join(parts[2:], "/")
		if f.FileInfo().IsDir() {
			if len(parts) != 3 {
				return nil, fmt.Errorf("invalid catalogue directory %q", rel)
			}
			dirs[rel] = true
			continue
		}
		if rel == "schema_embed.go" {
			continue
		}
		if f.UncompressedSize64 > catalogue.MaxBytes {
			return nil, fmt.Errorf("catalogue entry too large")
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(r, catalogue.MaxBytes+1))
		closeErr := r.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(b) > catalogue.MaxBytes {
			return nil, fmt.Errorf("catalogue entry too large")
		}
		files[rel] = b
	}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if regular[parent] {
				return nil, fmt.Errorf("archive file conflicts with directory %q", parent)
			}
		}
	}
	for dir := range dirs {
		if _, ok := files[dir+"/app.json"]; !ok {
			return nil, fmt.Errorf("missing descriptor for %s", dir)
		}
	}
	if _, ok := files["catalogue.schema.json"]; !ok {
		return nil, fmt.Errorf("missing catalogue schema")
	}
	return catalogue.ParseTree(files, version)
}
