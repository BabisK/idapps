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
	b, e = get(strings.TrimRight(raw, "/")+"/"+head.SHA+"/catalogue.json", catalogue.MaxBytes)
	if e != nil {
		return nil, fmt.Errorf("download catalogue: %w", e)
	}
	c, e := catalogue.Parse(b, version)
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256(b)
	return &Generation{head.SHA, hex.EncodeToString(sum[:]), b, c}, nil
}
