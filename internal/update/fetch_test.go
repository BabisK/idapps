package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(s string) *http.Response {
	return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(s)), Header: make(http.Header)}
}
func archiveFixture(files map[string][]byte) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range files {
		f, err := w.Create(name)
		Expect(err).NotTo(HaveOccurred())
		_, err = f.Write(data)
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(w.Close()).To(Succeed())
	return buf.Bytes()
}

var _ = Describe("fetching a pinned catalogue archive", func() {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	var files map[string][]byte
	var requests []string
	var body []byte
	BeforeEach(func() {
		files = map[string][]byte{}
		requests = nil
		Expect(filepath.WalkDir("../../catalogue", func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel("../../catalogue", p)
			if err != nil {
				return err
			}
			files["repo/catalogue/"+filepath.ToSlash(rel)], err = os.ReadFile(p)
			return err
		})).To(Succeed())
	})
	run := func() (*Generation, error) {
		client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, r.URL.Path)
			if r.URL.Path == "/main" {
				return response(fmt.Sprintf(`{"sha":%q}`, sha)), nil
			}
			if r.URL.Path == "/"+sha {
				return response(string(body)), nil
			}
			return nil, fmt.Errorf("unexpected request")
		})}
		return fetch(context.Background(), client, "1.0.0", "https://example.test/main", "https://example.test")
	}
	It("resolves the commit once and downloads its complete archive", func() {
		body = archiveFixture(files)
		g, err := run()
		Expect(err).NotTo(HaveOccurred())
		Expect(g.Commit).To(Equal(sha))
		Expect(g.Bytes).To(Equal(body))
		sum := sha256.Sum256(body)
		Expect(g.SHA256).To(Equal(hex.EncodeToString(sum[:])))
		parsed, err := parseArchive(g.Bytes, "1.0.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.Apps).To(HaveLen(22))
		Expect(requests).To(Equal([]string{"/main", "/" + sha}))
	})
	DescribeTable("rejects invalid archives", func(mutate func()) { mutate(); g, err := run(); Expect(g).To(BeNil()); Expect(err).To(HaveOccurred()) },
		Entry("truncated", func() { body = archiveFixture(files); body = body[:len(body)-8] }),
		Entry("unsafe path", func() { files["repo/../evil"] = []byte("x"); body = archiveFixture(files) }),
		Entry("invalid descriptor", func() { files["repo/catalogue/psql/app.json"] = []byte(`{"kind":`); body = archiveFixture(files) }),
		Entry("missing metadata", func() { delete(files, "repo/catalogue/metadata.json"); body = archiveFixture(files) }),
		Entry("missing schema", func() { delete(files, "repo/catalogue/catalogue.schema.json"); body = archiveFixture(files) }),
		Entry("ambiguous root", func() { files["other/foo"] = []byte("x"); body = archiveFixture(files) }),
	)
	It("rejects duplicate paths", func() {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		for i := 0; i < 2; i++ {
			f, err := w.Create("repo/catalogue/metadata.json")
			Expect(err).NotTo(HaveOccurred())
			_, err = f.Write([]byte("{}"))
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(w.Close()).To(Succeed())
		body = b.Bytes()
		_, err := run()
		Expect(err).To(MatchError(ContainSubstring("duplicate")))
	})
})

var _ = Describe("archive safety and HTTP failures", func() {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	It("rejects invalid commit responses and HTTP failures", func() {
		for _, tc := range []struct {
			main                      string
			mainStatus, archiveStatus int
		}{
			{`{"sha":"invalid"}`, 200, 200}, {"", 503, 200}, {fmt.Sprintf(`{"sha":%q}`, sha), 200, 404},
		} {
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				res := response(tc.main)
				res.StatusCode = tc.mainStatus
				res.Status = fmt.Sprintf("%d test", tc.mainStatus)
				if r.URL.Path != "/main" {
					res.StatusCode = tc.archiveStatus
					res.Status = fmt.Sprintf("%d test", tc.archiveStatus)
				}
				return res, nil
			})}
			_, err := fetch(context.Background(), client, "1.0.0", "https://example.test/main", "https://example.test")
			Expect(err).To(HaveOccurred())
		}
	})
	It("rejects oversized commit and archive responses", func() {
		for _, archive := range []bool{false, true} {
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/main" && archive {
					return response(fmt.Sprintf(`{"sha":%q}`, sha)), nil
				}
				size := 1 << 20
				if archive {
					size = maxArchiveBytes
				}
				return response(strings.Repeat("x", size+1)), nil
			})}
			_, err := fetch(context.Background(), client, "1.0.0", "https://example.test/main", "https://example.test")
			Expect(err).To(MatchError(ContainSubstring("size limit")))
		}
	})
	It("rejects advertised expanded sizes beyond the limit", func() {
		b := archiveFixture(map[string][]byte{"repo/file": []byte("x")})
		i := bytes.Index(b, []byte("PK\x01\x02"))
		Expect(i).To(BeNumerically(">=", 0))
		binary.LittleEndian.PutUint32(b[i+24:], maxExpandedBytes+1)
		_, err := parseArchive(b, "1.0.0")
		Expect(err).To(MatchError(ContainSubstring("expanded size")))
	})
	It("rejects the combined expanded size of multiple entries", func() {
		b := archiveFixture(map[string][]byte{"repo/one": []byte("x"), "repo/two": []byte("x")})
		first := bytes.Index(b, []byte("PK\x01\x02"))
		second := bytes.Index(b[first+4:], []byte("PK\x01\x02")) + first + 4
		Expect(first).To(BeNumerically(">=", 0))
		Expect(second).To(BeNumerically(">", first+4))
		binary.LittleEndian.PutUint32(b[first+24:], maxExpandedBytes/2+1)
		binary.LittleEndian.PutUint32(b[second+24:], maxExpandedBytes/2+1)
		_, err := parseArchiveMode(b, "1.0.0", false)
		Expect(err).To(MatchError(ContainSubstring("expanded size")))
	})
	It("rejects directory entries with payloads", func() {
		b := archiveFixture(map[string][]byte{"repo/dir/": nil})
		i := bytes.Index(b, []byte("PK\x01\x02"))
		binary.LittleEndian.PutUint32(b[i+24:], 1)
		_, err := parseArchive(b, "1.0.0")
		Expect(err).To(MatchError(ContainSubstring("directory has data")))
	})
	It("rejects symlinks and file-directory collisions", func() {
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		h := &zip.FileHeader{Name: "repo/link"}
		h.SetMode(os.ModeSymlink | 0777)
		_, err := w.CreateHeader(h)
		Expect(err).NotTo(HaveOccurred())
		Expect(w.Close()).To(Succeed())
		_, err = parseArchive(buf.Bytes(), "1.0.0")
		Expect(err).To(MatchError(ContainSubstring("unsafe archive entry")))
		b := archiveFixture(map[string][]byte{"repo/file": nil, "repo/file/child": nil})
		_, err = parseArchive(b, "1.0.0")
		Expect(err).To(MatchError(ContainSubstring("conflicts with directory")))
	})
	It("checks unrelated file CRC on update without decompressing it on snapshots", func() {
		files := map[string][]byte{}
		Expect(filepath.WalkDir("../../catalogue", func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, e := filepath.Rel("../../catalogue", p)
			if e != nil {
				return e
			}
			files["repo/catalogue/"+filepath.ToSlash(rel)], e = os.ReadFile(p)
			return e
		})).To(Succeed())
		files["repo/other"] = []byte("content")
		b := archiveFixture(files)
		z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		Expect(err).NotTo(HaveOccurred())
		for _, f := range z.File {
			if f.Name == "repo/other" {
				off, e := f.DataOffset()
				Expect(e).NotTo(HaveOccurred())
				b[off+int64(f.CompressedSize64)+4] ^= 1
				for i := 0; i+46+len(f.Name) <= len(b); i++ {
					if bytes.Equal(b[i:i+4], []byte("PK\x01\x02")) && string(b[i+46:i+46+len(f.Name)]) == f.Name {
						b[i+16] ^= 1
						break
					}
				}
			}
		}
		_, err = parseArchive(b, "1.0.0")
		Expect(err).To(HaveOccurred())
		_, err = parseArchiveMode(b, "1.0.0", false)
		Expect(err).NotTo(HaveOccurred())
	})
	DescribeTable("enforces trusted redirect origins", func(target string, hops int, allowed bool) {
		u, err := url.Parse(target)
		Expect(err).NotTo(HaveOccurred())
		via := make([]*http.Request, hops)
		err = checkRedirect(&http.Request{URL: u}, via)
		if allowed {
			Expect(err).NotTo(HaveOccurred())
		} else {
			Expect(err).To(HaveOccurred())
		}
	}, Entry("GitHub API", "https://api.github.com/path", 1, true), Entry("codeload", "https://codeload.github.com/path", 1, true), Entry("HTTP", "http://codeload.github.com/path", 1, false), Entry("foreign origin", "https://example.com/path", 1, false), Entry("too many", "https://api.github.com/path", 5, false))
})
