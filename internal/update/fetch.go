// Package update fetches and validates a pinned catalogue generation.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BabisK/idapps/internal/catalogue"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Generation struct {
	Commit, SHA256 string
	Bytes          []byte
	Catalogue      *catalogue.Catalogue
}

// Fetch resolves main once, then fetches the catalogue at that immutable commit.
func Fetch(ctx context.Context, version string) (*Generation, error) {
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || (req.URL.Host != "api.github.com" && req.URL.Host != "raw.githubusercontent.com") {
			return fmt.Errorf("refusing catalogue redirect to untrusted origin")
		}
		return nil
	}}
	return fetch(ctx, client, version, "https://api.github.com/repos/BabisK/idapps/commits/main", "https://raw.githubusercontent.com/BabisK/idapps/")
}
func fetch(ctx context.Context, client *http.Client, version, api, raw string) (*Generation, error) {
	get := func(url string, limit int64) ([]byte, error) {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if e != nil {
			return nil, e
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "idapps/"+version)
		res, e := client.Do(req)
		if e != nil {
			return nil, e
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: HTTP %s", url, res.Status)
		}
		b, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
		if e != nil {
			return nil, e
		}
		if int64(len(b)) > limit {
			return nil, fmt.Errorf("response exceeds size limit")
		}
		return b, nil
	}
	b, e := get(api, 1<<20)
	if e != nil {
		return nil, fmt.Errorf("resolve main: %w", e)
	}
	var head struct {
		SHA string `json:"sha"`
	}
	if e = json.Unmarshal(b, &head); e != nil || !commitPattern.MatchString(head.SHA) {
		return nil, fmt.Errorf("invalid main commit response")
	}
	base := strings.TrimSuffix(api, "/commits/main")
	type treeEntry struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Mode string `json:"mode"`
		SHA  string `json:"sha"`
	}
	type treeResponse struct {
		SHA       string      `json:"sha"`
		Truncated *bool       `json:"truncated"`
		Tree      []treeEntry `json:"tree"`
	}
	readTree := func(url string) (*treeResponse, error) {
		data, err := get(url, 5<<20)
		if err != nil {
			return nil, err
		}
		if err = catalogue.CheckJSON(data); err != nil {
			return nil, err
		}
		var tree treeResponse
		if err = json.Unmarshal(data, &tree); err != nil || !commitPattern.MatchString(tree.SHA) || tree.Truncated == nil || *tree.Truncated {
			return nil, fmt.Errorf("invalid or incomplete tree")
		}
		return &tree, nil
	}
	root, e := readTree(base + "/git/trees/" + head.SHA)
	if e != nil {
		return nil, fmt.Errorf("download root tree: %w", e)
	}
	var subtreeSHA string
	for _, entry := range root.Tree {
		if entry.Path != "catalogue" {
			continue
		}
		if subtreeSHA != "" || entry.Type != "tree" || entry.Mode != "040000" || !commitPattern.MatchString(entry.SHA) {
			return nil, fmt.Errorf("invalid catalogue tree")
		}
		subtreeSHA = entry.SHA
	}
	if subtreeSHA == "" {
		return nil, fmt.Errorf("missing catalogue tree")
	}
	tree, e := readTree(base + "/git/trees/" + subtreeSHA + "?recursive=1")
	if e != nil {
		return nil, fmt.Errorf("download catalogue tree: %w", e)
	}
	if tree.SHA != subtreeSHA {
		return nil, fmt.Errorf("catalogue tree SHA mismatch")
	}
	files := map[string][]byte{}
	dirs := map[string]bool{}
	var paths []string
	seen := map[string]bool{}
	for _, entry := range tree.Tree {
		if seen[entry.Path] {
			return nil, fmt.Errorf("duplicate tree path")
		}
		seen[entry.Path] = true
		rel := entry.Path
		if entry.Type == "tree" && entry.Mode == "040000" && !strings.Contains(rel, "/") {
			dirs[rel] = true
			continue
		}
		if entry.Type != "blob" || entry.Mode != "100644" {
			return nil, fmt.Errorf("invalid catalogue tree entry %q", rel)
		}
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	total := 0
	for _, p := range paths {
		// Validate paths before putting them into a request URL.
		parts := strings.Split(p, "/")
		if p != "metadata.json" && p != "catalogue.schema.json" && (len(parts) != 2 || !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`).MatchString(parts[0]) || parts[1] != "app.json" || !dirs[parts[0]]) {
			return nil, fmt.Errorf("invalid descriptor path %q", p)
		}
		data, err := get(strings.TrimRight(raw, "/")+"/"+head.SHA+"/catalogue/"+p, catalogue.MaxBytes-int64(total))
		if err != nil {
			return nil, fmt.Errorf("download %s: %w", p, err)
		}
		total += len(data)
		files[p] = data
	}
	for dir := range dirs {
		if _, ok := files[dir+"/app.json"]; !ok {
			return nil, fmt.Errorf("missing descriptor for %s", dir)
		}
	}
	b, e = catalogue.Assemble(files, version)
	if e != nil {
		return nil, e
	}
	c, e := catalogue.Parse(b, version)
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256(b)
	return &Generation{head.SHA, hex.EncodeToString(sum[:]), b, c}, nil
}
