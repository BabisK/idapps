package update

import (
	"context"
	"errors"
	"fmt"
	"github.com/BabisK/idapps/internal/catalogue"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"io"
	"net/http"
	"os"
	"strings"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(s string) *http.Response {
	return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(s)), Header: make(http.Header)}
}

var _ = Describe("fetching a catalogue", func() {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	var client *http.Client

	When("the main branch resolves to a valid commit", func() {
		BeforeEach(func() {
			catalogueBytes, err := os.ReadFile("../../catalogue.json")
			Expect(err).NotTo(HaveOccurred())
			client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/main":
					return response(fmt.Sprintf(`{"sha":%q}`, sha)), nil
				case "/" + sha + "/catalogue.json":
					return response(string(catalogueBytes)), nil
				default:
					return nil, fmt.Errorf("unexpected request %s", r.URL.Path)
				}
			})}
		})

		It("downloads and validates the catalogue at that commit", func() {
			generation, err := fetch(context.Background(), client, "1.0.0", "https://example.test/main", "https://example.test")
			Expect(err).NotTo(HaveOccurred())
			Expect(generation.Commit).To(Equal(sha))
			Expect(generation.SHA256).To(HaveLen(64))
			Expect(generation.Catalogue.Apps).To(HaveLen(22))
		})
	})

	When("the commit response contains an invalid identifier", func() {
		BeforeEach(func() {
			client = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				return response(`{"sha":"../../escape"}`), nil
			})}
		})

		It("rejects the response", func() {
			_, err := fetch(context.Background(), client, "1.0.0", "https://example.test", "https://example.test")
			Expect(err).To(MatchError(ContainSubstring("commit")))
		})
	})
})

var _ = Describe("downloading catalogue updates", func() {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	var document []byte
	BeforeEach(func() {
		var err error
		document, err = os.ReadFile("../../catalogue.json")
		Expect(err).NotTo(HaveOccurred())
	})
	When("main changes while the update is in progress", func() {
		It("fetches the catalogue at the original resolved commit", func() {
			var paths []string
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				paths = append(paths, r.URL.Path)
				if r.URL.Path == "/main" {
					return response(fmt.Sprintf(`{"sha":%q}`, commit)), nil
				}
				if r.URL.Path == "/"+commit+"/catalogue.json" {
					return response(string(document)), nil
				}
				return nil, fmt.Errorf("unexpected path %s", r.URL.Path)
			})}
			generation, err := fetch(context.Background(), client, "1.0.0", "https://example.test/main", "https://example.test")
			Expect(err).NotTo(HaveOccurred())
			Expect(generation.Commit).To(Equal(commit))
			Expect(paths).To(Equal([]string{"/main", "/" + commit + "/catalogue.json"}))
		})
	})
	When("a download fails or contains invalid data", func() {
		DescribeTable("rejects the update without returning a generation", func(body string, status int, failMain bool, transportError error, message string) {
			client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/main" && !failMain {
					return response(fmt.Sprintf(`{"sha":%q}`, commit)), nil
				}
				if transportError != nil {
					return nil, transportError
				}
				return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d test", status), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			generation, err := fetch(context.Background(), client, "1.0.0", "https://example.test/main", "https://example.test")
			Expect(generation).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
			Entry("main cannot be reached", "", 0, true, errors.New("connection refused"), "resolve main"),
			Entry("main returns an HTTP error", "", http.StatusServiceUnavailable, true, nil, "HTTP 503"),
			Entry("the catalogue returns an HTTP error", "", http.StatusNotFound, false, nil, "HTTP 404"),
			Entry("the catalogue exceeds the size limit", strings.Repeat("x", catalogue.MaxBytes+1), http.StatusOK, false, nil, "size limit"),
			Entry("the catalogue is truncated", `{"schemaVersion":`, http.StatusOK, false, nil, "EOF"),
			Entry("the catalogue contains duplicate keys", `{"schemaVersion":1,"schemaVersion":1}`, http.StatusOK, false, nil, "duplicate"),
			Entry("the catalogue uses an unsupported schema", `{"schemaVersion":2,"minimumLauncherVersion":"0.0.0","apps":{}}`, http.StatusOK, false, nil, "invalid catalogue"),
		)
	})
})
