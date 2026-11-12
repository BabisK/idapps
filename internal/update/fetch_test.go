package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/BabisK/idapps/internal/catalogue"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(s string) *http.Response {
	return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(s)), Header: make(http.Header)}
}

var _ = Describe("fetching a pinned catalogue tree", func() {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	const treeSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var entries []map[string]string
	var files map[string]string
	var requests []string
	var override func(*http.Request) *http.Response
	var client *http.Client
	BeforeEach(func() {
		entries = nil
		files = map[string]string{}
		requests = nil
		override = nil
		err := filepath.WalkDir("../../catalogue", func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if p == "../../catalogue" {
				return nil
			}
			rel, err := filepath.Rel("../../catalogue", p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			item := map[string]string{"path": rel, "type": "blob", "mode": "100644"}
			if d.IsDir() {
				item["type"] = "tree"
				item["mode"] = "040000"
			} else {
				b, e := os.ReadFile(p)
				if e != nil {
					return e
				}
				files[rel] = string(b)
			}
			entries = append(entries, item)
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
		client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, r.URL.RequestURI())
			if override != nil {
				if res := override(r); res != nil {
					return res, nil
				}
			}
			switch r.URL.RequestURI() {
			case "/main":
				return response(fmt.Sprintf(`{"sha":%q}`, sha)), nil
			case "/main/git/trees/" + sha:
				return response(fmt.Sprintf(`{"sha":%q,"truncated":false,"tree":[{"path":"catalogue","type":"tree","mode":"040000","sha":%q}]}`, sha, treeSHA)), nil
			case "/main/git/trees/" + treeSHA + "?recursive=1":
				b, _ := json.Marshal(map[string]any{"sha": treeSHA, "truncated": false, "tree": entries})
				return response(string(b)), nil
			}
			prefix := "/" + sha + "/catalogue/"
			if strings.HasPrefix(r.URL.Path, prefix) {
				return response(files[strings.TrimPrefix(r.URL.Path, prefix)]), nil
			}
			return nil, fmt.Errorf("unexpected request %s", r.URL)
		})}
	})
	run := func() (*Generation, error) {
		return fetch(context.Background(), client, "1.0.0", "https://example.test/main", "https://example.test")
	}
	It("resolves main once and fetches the subtree and descriptors at the pinned commit", func() {
		g, err := run()
		Expect(err).NotTo(HaveOccurred())
		Expect(g.Commit).To(Equal(sha))
		Expect(g.Catalogue.Apps).To(HaveLen(22))
		Expect(g.SHA256).To(HaveLen(64))
		Expect(requests[:3]).To(Equal([]string{"/main", "/main/git/trees/" + sha, "/main/git/trees/" + treeSHA + "?recursive=1"}))
		for _, p := range requests[3:] {
			Expect(p).To(HavePrefix("/" + sha + "/catalogue/"))
		}
	})
	DescribeTable("rejects invalid responses without returning a generation", func(path, body, message string, status int) {
		override = func(r *http.Request) *http.Response {
			if r.URL.RequestURI() != path {
				return nil
			}
			res := response(body)
			if status != 0 {
				res.StatusCode = status
				res.Status = fmt.Sprintf("%d test", status)
			}
			return res
		}
		g, err := run()
		Expect(g).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring(message)))
	},
		Entry("invalid commit", "/main", `{"sha":"../invalid"}`, "commit", 0),
		Entry("main HTTP failure", "/main", "", "HTTP 503", 503),
		Entry("root tree HTTP failure", "/main/git/trees/"+sha, "", "HTTP 404", 404),
		Entry("descriptor HTTP failure", "/"+sha+"/catalogue/psql/app.json", "", "HTTP 404", 404),
		Entry("oversized descriptor", "/"+sha+"/catalogue/psql/app.json", strings.Repeat("x", catalogue.MaxBytes+1), "size limit", 0),
		Entry("malformed descriptor", "/"+sha+"/catalogue/psql/app.json", `{"kind":`, "EOF", 0),
		Entry("duplicate descriptor key", "/"+sha+"/catalogue/psql/app.json", `{"kind":"command","kind":"service"}`, "duplicate", 0),
		Entry("unsupported schema", "/"+sha+"/catalogue/metadata.json", `{"schemaVersion":2,"minimumLauncherVersion":"0.0.0"}`, "invalid catalogue", 0),
		Entry("missing root completeness indicator", "/main/git/trees/"+sha, fmt.Sprintf(`{"sha":%q,"tree":[]}`, sha), "incomplete tree", 0),
		Entry("missing subtree completeness indicator", "/main/git/trees/"+treeSHA+"?recursive=1", fmt.Sprintf(`{"sha":%q,"tree":[]}`, treeSHA), "incomplete tree", 0),
		Entry("truncated subtree", "/main/git/trees/"+treeSHA+"?recursive=1", fmt.Sprintf(`{"sha":%q,"truncated":true,"tree":[]}`, treeSHA), "incomplete tree", 0),
	)
	It("rejects duplicate tree entries", func() {
		entries = append(entries, entries[0])
		g, err := run()
		Expect(g).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("duplicate")))
	})
})
