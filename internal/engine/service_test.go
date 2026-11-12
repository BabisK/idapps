package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/BabisK/idapps/internal/catalogue"
	"github.com/BabisK/idapps/internal/storage"
	"github.com/BabisK/idapps/internal/update"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type serviceTransport func(*http.Request) (*http.Response, error)

func (f serviceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

var _ = Describe("service lifecycle", func() {
	Context("when parsing log options", func() {
		It("accepts follow and a tail count", func() {
			f, tail, e := logOptions([]string{"-f", "-t", "20", "--"})
			Expect(e).NotTo(HaveOccurred())
			Expect(f).To(BeTrue())
			Expect(tail).To(Equal("20"))
		})
		DescribeTable("rejects invalid options", func(args []string) {
			_, _, err := logOptions(args)
			Expect(err).To(HaveOccurred())
		},
			Entry("a missing tail count", []string{"-t"}),
			Entry("a negative tail count", []string{"-t", "-1"}),
			Entry("an operand after the separator", []string{"--", "operand"}),
			Entry("an unexpected operand", []string{"operand"}),
		)
	})
	Context("when checking ownership", func() {
		DescribeTable("requires matching installation and app labels", func(labels map[string]string, expected bool) {
			Expect(owned(labels, "id", "postgres")).To(Equal(expected))
		},
			Entry("missing labels", nil, false),
			Entry("a different installation", map[string]string{ownerLabel: "other", appLabel: "postgres"}, false),
			Entry("a different application", map[string]string{ownerLabel: "id", appLabel: "redis"}, false),
			Entry("matching labels", map[string]string{ownerLabel: "id", appLabel: "postgres"}, true),
		)
	})
	Context("when protecting existing resources", func() {
		var store *update.Store
		BeforeEach(func() {
			t := GinkgoT()
			paths := storage.Paths{Cache: filepath.Join(t.TempDir(), "idapps", "catalogues"), Config: filepath.Join(t.TempDir(), "idapps"), Bin: filepath.Join(t.TempDir(), "idapps", "bin")}
			var err error
			store, err = update.NewStore(paths, "1.0.0")
			Expect(err).NotTo(HaveOccurred())
			previous := openEngine
			DeferCleanup(func() { openEngine = previous })
		})
		It("refuses to start with a foreign volume", func() {
			var e error

			var writes []string
			rt := serviceTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					writes = append(writes, r.Method+" "+r.URL.Path)
				}
				status := 200
				body := `{}`
				switch {
				case strings.HasSuffix(r.URL.Path, "/info"):
					body = `{"ID":"engine-1","OSType":"linux"}`
				case strings.Contains(r.URL.Path, "/containers/"):
					status = 404
					body = `{"message":"no such container"}`
				case strings.Contains(r.URL.Path, "/volumes/"):
					body = `{"Name":"data","Labels":{}}`
				}
				return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d", status), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})
			openEngine = func(context.Context) (*client.Client, error) {
				return client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: rt}))
			}
			app := catalogue.App{Kind: "service", ContainerName: "db", Image: catalogue.Image{Repository: "postgres", Tag: "1"}, Mounts: []catalogue.Mount{{Type: "volume", Name: "data", Target: "/data"}}}
			app = validServiceFixture(app)
			e = store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", &app, nil) })
			Expect(e).To(MatchError(ContainSubstring("not owned")))
			Expect(writes).To(BeEmpty(), "engine must not be modified")
			Expect(store.WithLock(context.Background(), func() error {
				_, records, err := store.Resources()
				if err == nil {
					Expect(records).NotTo(HaveKey("db"))
				}
				return err
			})).To(Succeed())

		})
		It("refuses to stop a foreign container", func() {
			var e error

			var writes []string
			rt := serviceTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					writes = append(writes, r.Method+" "+r.URL.Path)
				}
				body := `{}`
				switch {
				case strings.HasSuffix(r.URL.Path, "/info"):
					body = `{"ID":"engine-1","OSType":"linux"}`
				case strings.Contains(r.URL.Path, "/containers/"):
					body = `{"Id":"foreign-id","Config":{"Labels":{}},"State":{"Running":true}}`
				}
				return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})
			openEngine = func(context.Context) (*client.Client, error) {
				return client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: rt}))
			}
			app := catalogue.App{Kind: "service", ContainerName: "db"}
			app = validServiceFixture(app)
			e = store.WithLock(context.Background(), func() error {
				id, records, e := store.Resources()
				if e != nil {
					return e
				}
				records["db"] = update.ServiceRecord{App: "db", Engine: "engine-1", Definition: app}
				if e = store.SaveResources(id, records); e != nil {
					return e
				}
				return Service(context.Background(), store, "db", nil, []string{"stop"})
			})
			Expect(e).To(MatchError(ContainSubstring("not owned")))
			Expect(writes).To(BeEmpty(), "engine must not be modified")

		})
		It("refuses to clean an in-use volume", func() {
			var e error

			var identity string
			var writes []string
			rt := serviceTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					writes = append(writes, r.Method+" "+r.URL.Path)
				}
				status := 200
				body := `{}`
				switch {
				case strings.HasSuffix(r.URL.Path, "/info"):
					body = `{"ID":"engine-1","OSType":"linux"}`
				case strings.HasSuffix(r.URL.Path, "/containers/db/json"):
					status = 404
					body = `{"message":"no such container"}`
				case strings.Contains(r.URL.Path, "/volumes/"):
					body = fmt.Sprintf(`{"Name":"data","Labels":{"io.idapps.installation":%q,"io.idapps.app":"db"}}`, identity)
				case strings.HasSuffix(r.URL.Path, "/containers/json"):
					body = `[{"Id":"unrelated","Mounts":[{"Name":"data"}]}]`
				}
				return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d", status), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})
			openEngine = func(context.Context) (*client.Client, error) {
				return client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: rt}))
			}
			app := catalogue.App{Kind: "service", ContainerName: "db", Mounts: []catalogue.Mount{{Type: "volume", Name: "data"}}}
			app = validServiceFixture(app)
			e = store.WithLock(context.Background(), func() error {
				id, records, e := store.Resources()
				if e != nil {
					return e
				}
				identity = id.Installation
				records["db"] = update.ServiceRecord{App: "db", Engine: "engine-1", Definition: app}
				if e = store.SaveResources(id, records); e != nil {
					return e
				}
				return Service(context.Background(), store, "db", nil, []string{"clean"})
			})
			Expect(e).To(MatchError(ContainSubstring("in use")))
			Expect(writes).To(BeEmpty(), "engine must not be modified")

		})
		It("rejects incomplete owned-container inspection data", func() {
			var err error

			var installation string
			var mutations []string
			rt := serviceTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					mutations = append(mutations, r.Method+" "+r.URL.Path)
				}
				body := `{}`
				switch {
				case strings.HasSuffix(r.URL.Path, "/info"):
					body = `{"ID":"engine-1","OSType":"linux"}`
				case strings.HasSuffix(r.URL.Path, "/containers/db/json"):
					body = fmt.Sprintf(`{"Id":"owned","Config":{"Labels":{"io.idapps.installation":%q,"io.idapps.app":"db"}}}`, installation)
				default:
					return nil, fmt.Errorf("unexpected request %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})
			openEngine = func(context.Context) (*client.Client, error) {
				return client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: rt}))
			}
			app := catalogue.App{Kind: "service", ContainerName: "db"}
			app = validServiceFixture(app)
			err = store.WithLock(context.Background(), func() error {
				id, records, e := store.Resources()
				if e != nil {
					return e
				}
				installation = id.Installation
				records["db"] = update.ServiceRecord{App: "db", Engine: "engine-1", ContainerID: "owned", Definition: app}
				if e = store.SaveResources(id, records); e != nil {
					return e
				}
				return Service(context.Background(), store, "db", nil, []string{"stop"})
			})
			Expect(err).To(MatchError(ContainSubstring("incomplete state")))
			Expect(mutations).To(BeEmpty(), "engine must not be modified")

		})
	})
	Context("when starting a service", func() {
		It("creates and records owned service resources", func() {
			t := GinkgoT()
			paths := storage.Paths{Cache: filepath.Join(t.TempDir(), "idapps", "catalogues"), Config: filepath.Join(t.TempDir(), "idapps"), Bin: filepath.Join(t.TempDir(), "idapps", "bin")}
			store, e := update.NewStore(paths, "1.0.0")
			Expect(e).NotTo(HaveOccurred())

			previous := openEngine
			defer func() { openEngine = previous }()
			var create map[string]any
			var volumeCreate map[string]any
			var requests []string
			rt := serviceTransport(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				status := 200
				body := `{}`
				switch {
				case strings.HasSuffix(r.URL.Path, "/info"):
					body = `{"ID":"engine-1","OSType":"linux"}`
				case strings.HasSuffix(r.URL.Path, "/containers/db/json"), r.Method == "GET" && strings.Contains(r.URL.Path, "/volumes/"):
					status = 404
					body = `{"message":"missing"}`
				case strings.HasSuffix(r.URL.Path, "/volumes/create"):
					b, _ := io.ReadAll(r.Body)
					if e := json.Unmarshal(b, &volumeCreate); e != nil {
						t.Error(e)
					}
					body = string(b)
				case strings.HasSuffix(r.URL.Path, "/containers/create"):
					b, _ := io.ReadAll(r.Body)
					if e := json.Unmarshal(b, &create); e != nil {
						t.Error(e)
					}
					body = `{"Id":"container-id"}`
				case strings.HasSuffix(r.URL.Path, "/start"):
					status = 204
					body = ""
				}
				return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d", status), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})
			openEngine = func(context.Context) (*client.Client, error) {
				return client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: rt}))
			}
			app := catalogue.App{Kind: "service", ContainerName: "db", Image: catalogue.Image{Repository: "postgres", Tag: "1"}, Network: "bridge", Mounts: []catalogue.Mount{{Type: "volume", Name: "data", Target: "/data"}}, Ports: []catalogue.Port{{HostIP: "127.0.0.1", HostPort: 5432, ContainerPort: 5432, Protocol: "tcp"}}}
			app = validServiceFixture(app)
			e = store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", &app, nil) })
			Expect(e).NotTo(HaveOccurred())

			labels, ok := create["Labels"].(map[string]any)
			Expect(ok).To(BeTrue(), "container labels: %#v", create)
			Expect(labels[appLabel]).To(Equal("db"))
			Expect(labels[ownerLabel]).NotTo(BeEmpty())

			volLabels, ok := volumeCreate["Labels"].(map[string]any)
			Expect(ok).To(BeTrue(), "volume: %#v", volumeCreate)
			Expect(volLabels[ownerLabel]).To(Equal(labels[ownerLabel]))
			Expect(volumeCreate["Name"]).To(Equal("data"))

			host, ok := create["HostConfig"].(map[string]any)
			Expect(ok).To(BeTrue(), "host config: %#v", create)
			Expect(host["AutoRemove"]).To(BeTrue())
			Expect(host["PortBindings"]).NotTo(BeNil())

			_, records, e := store.Resources()
			Expect(e).NotTo(HaveOccurred())
			Expect(records["db"].ContainerID).To(Equal("container-id"))

		})
	})
	Context("when following service logs", func() {
		It("permits update identity checks while logs are followed", func() {
			t := GinkgoT()
			base := t.TempDir()
			paths := storage.Paths{Cache: filepath.Join(base, "cache", "idapps", "catalogues"), Config: filepath.Join(base, "config", "idapps"), Bin: filepath.Join(base, "data", "idapps", "bin")}
			store, e := update.NewStore(paths, "1.0.0")
			Expect(e).NotTo(HaveOccurred())

			b, e := catalogue.ReadTree("../../catalogue", "1.0.0")
			Expect(e).NotTo(HaveOccurred())

			cat, e := catalogue.Parse(b, "1.0.0")
			Expect(e).NotTo(HaveOccurred())

			app := cat.Apps["postgres"]
			var identity string
			e = store.WithLock(context.Background(), func() error {
				id, records, e := store.Resources()
				if e != nil {
					return e
				}
				identity = id.Installation
				records["postgres"] = update.ServiceRecord{App: "postgres", Engine: "engine-1", ContainerID: "owned-container", Definition: app}
				return store.SaveResources(id, records)
			})
			Expect(e).NotTo(HaveOccurred())

			var doc map[string]any
			{
				e = json.Unmarshal(b, &doc)
				Expect(e).NotTo(HaveOccurred())
			}

			doc["apps"].(map[string]any)["postgres"].(map[string]any)["containerName"] = "changed-postgres"
			changed, e := json.Marshal(doc)
			Expect(e).NotTo(HaveOccurred())

			sum := sha256.Sum256(changed)
			gen := &update.Generation{Commit: strings.Repeat("a", 40), SHA256: hex.EncodeToString(sum[:]), Bytes: changed}
			reader, writer := io.Pipe()
			defer writer.Close()
			started := make(chan struct{})
			rt := serviceTransport(func(r *http.Request) (*http.Response, error) {
				body := `{}`
				switch {
				case strings.HasSuffix(r.URL.Path, "/info"):
					body = `{"ID":"engine-1","OSType":"linux"}`
				case strings.HasSuffix(r.URL.Path, "/containers/postgres/json"):
					body = fmt.Sprintf(`{"Id":"owned-container","Config":{"Labels":{"io.idapps.installation":%q,"io.idapps.app":"postgres"}},"State":{"Running":true}}`, identity)
				case strings.HasSuffix(r.URL.Path, "/containers/owned-container/logs"):
					close(started)
					return &http.Response{StatusCode: 200, Status: "200 OK", Body: reader, Header: make(http.Header), Request: r}, nil
				default:
					return nil, fmt.Errorf("unexpected Docker request: %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})
			previous := openEngine
			defer func() { openEngine = previous }()
			openEngine = func(context.Context) (*client.Client, error) {
				return client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: rt}))
			}
			done := make(chan error, 1)
			go func() { done <- ServiceLogs(context.Background(), store, "postgres", []string{"-f"}) }()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("log follow did not start")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := store.WithLock(ctx, func() error { return store.Activate(gen, filepath.Join(base, "idapps-executable")) })
			Expect(err).To(MatchError(ContainSubstring("different identity")))

			select {
			case e := <-done:
				t.Fatalf("log follow ended before its stream closed: %v", e)
			default:
			}
			writer.Close()
			select {
			case e := <-done:
				Expect(e).NotTo(HaveOccurred())

			case <-time.After(3 * time.Second):
				t.Fatal("log follow did not stop")
			}
		})
	})
})

var _ = Describe("Recovering owned service resources", func() {
	var transport *MockRoundTripper
	var store *update.Store
	var installation string
	var definition catalogue.App
	BeforeEach(func() {
		base := GinkgoT().TempDir()
		var err error
		store, err = update.NewStore(storage.Paths{Cache: filepath.Join(base, "cache", "idapps", "catalogues"), Config: filepath.Join(base, "config", "idapps"), Bin: filepath.Join(base, "data", "idapps", "bin")}, "0.0.0")
		Expect(err).NotTo(HaveOccurred())
		definition = catalogue.App{Kind: "service", ContainerName: "db", Mounts: []catalogue.Mount{{Type: "volume", Name: "db-data", Target: "/data"}}}
		definition = validServiceFixture(definition)
		Expect(store.WithLock(context.Background(), func() error {
			id, records, err := store.Resources()
			if err != nil {
				return err
			}
			installation = id.Installation
			records["db"] = update.ServiceRecord{App: "db", Engine: "engine-1", ContainerID: "owned-container", Definition: definition}
			return store.SaveResources(id, records)
		})).To(Succeed())
		transport = NewMockRoundTripper(gomock.NewController(GinkgoT()))
		previous := openEngine
		DeferCleanup(func() { openEngine = previous })
		openEngine = func(context.Context) (*client.Client, error) { return dockerClient(transport), nil }
		transport.EXPECT().RoundTrip(dockerRequest("GET", "/info")).Return(dockerResponse(http.StatusOK, `{"ID":"engine-1","OSType":"linux"}`), nil)
	})
	When("a labelled container was created before its ID was recorded", func() {
		It("recognizes a recovered running container when starting", func() {
			Expect(store.WithLock(context.Background(), func() error {
				id, records, err := store.Resources()
				if err != nil {
					return err
				}
				rec := records["db"]
				rec.ContainerID = ""
				records["db"] = rec
				return store.SaveResources(id, records)
			})).To(Succeed())
			body := fmt.Sprintf(`{"Id":"recovered","Config":{"Labels":{"io.idapps.installation":%q,"io.idapps.app":"db"}},"State":{"Running":true}}`, installation)
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/containers/db/json")).Return(dockerResponse(http.StatusOK, body), nil)
			err := store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", &definition, []string{"start"}) })
			Expect(err).To(MatchError(ContainSubstring("already running")))
		})
		It("recovers ownership and stops it without deleting the volume record", func() {
			Expect(store.WithLock(context.Background(), func() error {
				id, records, err := store.Resources()
				if err != nil {
					return err
				}
				rec := records["db"]
				rec.ContainerID = ""
				records["db"] = rec
				return store.SaveResources(id, records)
			})).To(Succeed())
			body := fmt.Sprintf(`{"Id":"recovered","Config":{"Labels":{"io.idapps.installation":%q,"io.idapps.app":"db"}},"State":{"Running":true}}`, installation)
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/containers/db/json")).Return(dockerResponse(http.StatusOK, body), nil)
			transport.EXPECT().RoundTrip(dockerRequest("POST", "/containers/recovered/stop")).Return(dockerResponse(http.StatusNoContent, ""), nil)
			Expect(store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", nil, []string{"stop"}) })).To(Succeed())
			Expect(store.WithLock(context.Background(), func() error {
				_, records, err := store.Resources()
				if err == nil {
					Expect(records).To(HaveKey("db"))
				}
				return err
			})).To(Succeed())
		})
	})
	When("the container name now points to a different owned container", func() {
		It("refuses to remove the stopped replacement when starting", func() {
			body := fmt.Sprintf(`{"Id":"replacement","Config":{"Labels":{"io.idapps.installation":%q,"io.idapps.app":"db"}},"State":{"Running":false}}`, installation)
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/containers/db/json")).Return(dockerResponse(http.StatusOK, body), nil)
			err := store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", &definition, []string{"start"}) })
			Expect(err).To(MatchError(ContainSubstring("recorded container identity does not match")))
		})
		It("refuses to stop the replacement", func() {
			body := fmt.Sprintf(`{"Id":"replacement","Config":{"Labels":{"io.idapps.installation":%q,"io.idapps.app":"db"}},"State":{"Running":true}}`, installation)
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/containers/db/json")).Return(dockerResponse(http.StatusOK, body), nil)
			err := store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", nil, []string{"stop"}) })
			Expect(err).To(MatchError(ContainSubstring("recorded container identity does not match")))
		})
	})
	When("a stopped owned service has a foreign volume", func() {
		It("refuses the start without removing the container or changing the record", func() {
			body := fmt.Sprintf(`{"Id":"owned-container","Config":{"Labels":{"io.idapps.installation":%q,"io.idapps.app":"db"}},"State":{"Running":false}}`, installation)
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/containers/db/json")).Return(dockerResponse(http.StatusOK, body), nil)
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/volumes/db-data")).Return(dockerResponse(http.StatusOK, `{"Name":"db-data","Labels":{}}`), nil)
			err := store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", &definition, []string{"start"}) })
			Expect(err).To(MatchError(ContainSubstring("not owned")))
			Expect(store.WithLock(context.Background(), func() error {
				_, records, err := store.Resources()
				if err == nil {
					Expect(records["db"].ContainerID).To(Equal("owned-container"))
				}
				return err
			})).To(Succeed())
		})
	})
	When("cleaning a retired service with no container", func() {
		BeforeEach(func() {
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/containers/db/json")).Return(dockerResponse(http.StatusNotFound, `{"message":"not found"}`), nil)
		})
		It("refuses to delete a foreign volume", func() {
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/containers/json")).Return(dockerResponse(http.StatusOK, `[]`), nil)
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/volumes/db-data")).Return(dockerResponse(http.StatusOK, `{"Name":"db-data","Labels":{"io.idapps.installation":"other","io.idapps.app":"db"}}`), nil)
			err := store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", nil, []string{"clean"}) })
			Expect(err).To(MatchError(ContainSubstring("not owned")))
			Expect(err).To(MatchError(ContainSubstring("stale service record removed")))
			Expect(store.WithLock(context.Background(), func() error {
				_, records, err := store.Resources()
				if err == nil {
					Expect(records).NotTo(HaveKey("db"))
				}
				return err
			})).To(Succeed())
		})
		It("retains the record when another owned volume remains", func() {
			Expect(store.WithLock(context.Background(), func() error {
				id, records, err := store.Resources()
				if err != nil {
					return err
				}
				rec := records["db"]
				rec.Definition.Mounts = append(rec.Definition.Mounts, catalogue.Mount{Type: "volume", Name: "owned-data", Target: "/other"})
				records["db"] = rec
				return store.SaveResources(id, records)
			})).To(Succeed())
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/containers/json")).Return(dockerResponse(http.StatusOK, `[]`), nil)
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/volumes/db-data")).Return(dockerResponse(http.StatusOK, `{"Name":"db-data","Labels":{}}`), nil)
			body := fmt.Sprintf(`{"Name":"owned-data","Labels":{"io.idapps.installation":%q,"io.idapps.app":"db"}}`, installation)
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/volumes/owned-data")).Return(dockerResponse(http.StatusOK, body), nil)
			err := store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", nil, []string{"clean"}) })
			Expect(err).To(MatchError(ContainSubstring("not owned")))
			Expect(store.WithLock(context.Background(), func() error {
				_, records, err := store.Resources()
				if err == nil {
					Expect(records).To(HaveKey("db"))
				}
				return err
			})).To(Succeed())
		})
		It("refuses to delete a volume in use by another container", func() {
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/containers/json")).Return(dockerResponse(http.StatusOK, `[{"Id":"other","Mounts":[{"Name":"db-data"}]}]`), nil)
			body := fmt.Sprintf(`{"Name":"db-data","Labels":{"io.idapps.installation":%q,"io.idapps.app":"db"}}`, installation)
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/volumes/db-data")).Return(dockerResponse(http.StatusOK, body), nil)
			err := store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", nil, []string{"clean"}) })
			Expect(err).To(MatchError(ContainSubstring("in use")))
		})
		It("removes only the owned unused volume and its retired record", func() {
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/containers/json")).Return(dockerResponse(http.StatusOK, `[]`), nil)
			body := fmt.Sprintf(`{"Name":"db-data","Labels":{"io.idapps.installation":%q,"io.idapps.app":"db"}}`, installation)
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/volumes/db-data")).Return(dockerResponse(http.StatusOK, body), nil)
			transport.EXPECT().RoundTrip(dockerRequest("DELETE", "/volumes/db-data")).Return(dockerResponse(http.StatusNoContent, ""), nil)
			Expect(store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", nil, []string{"clean"}) })).To(Succeed())
			Expect(store.WithLock(context.Background(), func() error {
				_, records, err := store.Resources()
				if err == nil {
					Expect(records).NotTo(HaveKey("db"))
				}
				return err
			})).To(Succeed())
		})
	})
	When("a service container has already been removed", func() {
		BeforeEach(func() {
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/containers/db/json")).Return(dockerResponse(http.StatusNotFound, `{"message":"not found"}`), nil)
		})
		It("clears the record on stop when no named volumes remain", func() {
			Expect(store.WithLock(context.Background(), func() error {
				id, records, err := store.Resources()
				if err != nil {
					return err
				}
				rec := records["db"]
				rec.Definition.Mounts = nil
				records["db"] = rec
				return store.SaveResources(id, records)
			})).To(Succeed())
			Expect(store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", nil, []string{"stop"}) })).To(Succeed())
			Expect(store.WithLock(context.Background(), func() error {
				_, records, err := store.Resources()
				if err == nil {
					Expect(records).NotTo(HaveKey("db"))
				}
				return err
			})).To(Succeed())
		})
		It("retains the record when named volumes may remain", func() {
			err := store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", nil, []string{"stop"}) })
			Expect(err).To(MatchError(ContainSubstring("has no container")))
			Expect(store.WithLock(context.Background(), func() error {
				_, records, err := store.Resources()
				if err == nil {
					Expect(records).To(HaveKey("db"))
				}
				return err
			})).To(Succeed())
		})
	})
})

type serviceExpectation struct {
	name, repository, containerName string
	entrypoint, args, environment   []string
	ports                           map[int]int
	volumes                         map[string]string
}

var _ = Describe("Catalogue service Docker requests", func() {
	var apps map[string]catalogue.App
	var mock *MockRoundTripper
	var store *update.Store
	BeforeEach(func() {
		document, err := catalogue.ReadTree("../../catalogue", "1.0.0")
		Expect(err).NotTo(HaveOccurred())
		parsed, err := catalogue.Parse(document, "0.0.0")
		Expect(err).NotTo(HaveOccurred())
		apps = parsed.Apps
		base := GinkgoT().TempDir()
		store, err = update.NewStore(storage.Paths{Cache: filepath.Join(base, "cache", "idapps", "catalogues"), Config: filepath.Join(base, "config", "idapps"), Bin: filepath.Join(base, "data", "idapps", "bin")}, "0.0.0")
		Expect(err).NotTo(HaveOccurred())
		mock = NewMockRoundTripper(gomock.NewController(GinkgoT()))
		previous := openEngine
		DeferCleanup(func() { openEngine = previous })
		openEngine = func(context.Context) (*client.Client, error) { return dockerClient(mock), nil }
	})
	When("a service is started", func() {
		DescribeTable("creates its approved resources with ownership labels", func(want serviceExpectation) {
			app := apps[want.name]
			Expect(app.Kind).To(Equal("service"))
			Expect(app.Image.Repository).To(Equal(want.repository))
			Expect(app.ContainerName).To(Equal(want.containerName))
			mock.EXPECT().RoundTrip(dockerRequest(http.MethodGet, "/info")).Return(dockerResponse(http.StatusOK, `{"ID":"engine-1","OSType":"linux"}`), nil)
			mock.EXPECT().RoundTrip(dockerRequest(http.MethodGet, "/containers/"+want.containerName+"/json")).Return(dockerResponse(http.StatusNotFound, `{"message":"not found"}`), nil)
			mock.EXPECT().RoundTrip(gomock.Cond(func(req *http.Request) bool {
				return req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/images/") && strings.HasSuffix(req.URL.Path, "/json")
			})).Return(dockerResponse(http.StatusOK, `{}`), nil)
			mock.EXPECT().RoundTrip(gomock.Cond(func(req *http.Request) bool {
				return req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/volumes/")
			})).Return(dockerResponse(http.StatusNotFound, `{"message":"not found"}`), nil).Times(len(want.volumes))
			mock.EXPECT().RoundTrip(dockerRequest(http.MethodPost, "/volumes/create")).DoAndReturn(func(req *http.Request) (*http.Response, error) {
				data, err := io.ReadAll(req.Body)
				Expect(err).NotTo(HaveOccurred())
				var volume struct {
					Name   string            `json:"Name"`
					Labels map[string]string `json:"Labels"`
				}
				Expect(json.Unmarshal(data, &volume)).To(Succeed())
				Expect(want.volumes).To(HaveKey(volume.Name))
				Expect(volume.Labels[appLabel]).To(Equal(want.name))
				Expect(volume.Labels[ownerLabel]).NotTo(BeEmpty())
				return dockerResponse(http.StatusCreated, string(data)), nil
			}).Times(len(want.volumes))
			var request struct {
				Image      string               `json:"Image"`
				Cmd        []string             `json:"Cmd"`
				Entrypoint []string             `json:"Entrypoint"`
				Env        []string             `json:"Env"`
				Labels     map[string]string    `json:"Labels"`
				HostConfig container.HostConfig `json:"HostConfig"`
			}
			mock.EXPECT().RoundTrip(dockerRequest(http.MethodPost, "/containers/create")).DoAndReturn(func(req *http.Request) (*http.Response, error) {
				Expect(req.URL.Query().Get("name")).To(Equal(want.containerName))
				data, err := io.ReadAll(req.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(json.Unmarshal(data, &request)).To(Succeed())
				return dockerResponse(http.StatusInternalServerError, `{"message":"test stops after create"}`), nil
			})
			err := store.WithLock(context.Background(), func() error { return Service(context.Background(), store, want.name, &app, nil) })
			Expect(err).To(MatchError(ContainSubstring("test stops after create")))
			Expect(request.Image).To(Equal(app.Image.Reference()))
			Expect(request.Entrypoint).To(Equal(want.entrypoint))
			Expect(request.Cmd).To(Equal(want.args))
			Expect(request.Env).To(ConsistOf(want.environment))
			Expect(request.Labels[appLabel]).To(Equal(want.name))
			Expect(request.Labels[ownerLabel]).NotTo(BeEmpty())
			Expect(string(request.HostConfig.NetworkMode)).To(Equal("bridge"))
			Expect(request.HostConfig.AutoRemove).To(BeTrue())
			Expect(request.HostConfig.PortBindings).To(HaveLen(len(want.ports)))
			for hostPort, containerPort := range want.ports {
				port := nat.Port(strconv.Itoa(containerPort) + "/tcp")
				Expect(request.HostConfig.PortBindings[port]).To(Equal([]nat.PortBinding{{HostIP: "0.0.0.0", HostPort: strconv.Itoa(hostPort)}}))
			}
			Expect(request.HostConfig.Mounts).To(HaveLen(len(want.volumes)))
			for _, binding := range request.HostConfig.Mounts {
				Expect(binding.Type).To(Equal(mount.TypeVolume))
				Expect(binding.Target).To(Equal(want.volumes[binding.Source]))
				Expect(binding.ReadOnly).To(BeFalse())
			}
		},
			Entry("etcd", serviceExpectation{name: "etcd", repository: "gcr.io/etcd-development/etcd", containerName: "etcd", entrypoint: []string{"/usr/local/bin/etcd"}, args: []string{"--listen-client-urls", "http://0.0.0.0:2379", "--advertise-client-urls", "http://0.0.0.0:2379"}, ports: map[int]int{2379: 2379, 2380: 2380}, volumes: map[string]string{"etcd": "/etcd-data"}}),
			Entry("mariadb", serviceExpectation{name: "mariadb-server", repository: "mariadb", containerName: "mariadb", environment: []string{"MARIADB_ROOT_PASSWORD=root"}, ports: map[int]int{3306: 3306}, volumes: map[string]string{"mariadb": "/var/lib/mysql"}}),
			Entry("memcached", serviceExpectation{name: "memcached", repository: "memcached", containerName: "memcached", ports: map[int]int{11211: 11211}}),
			Entry("mysql", serviceExpectation{name: "mysql-server", repository: "mysql", containerName: "mysql", environment: []string{"MYSQL_ROOT_PASSWORD=root"}, ports: map[int]int{3306: 3306}, volumes: map[string]string{"mysql": "/var/lib/mysql"}}),
			Entry("nats", serviceExpectation{name: "nats-server", repository: "nats", containerName: "nats-server", ports: map[int]int{4222: 4222, 6222: 6222, 8222: 8222}}),
			Entry("pgadmin", serviceExpectation{name: "pgadmin4", repository: "dpage/pgadmin4", containerName: "pgadmin4", environment: []string{"PGADMIN_DEFAULT_EMAIL=user@pgadmin.org", "PGADMIN_DEFAULT_PASSWORD=pgadmin"}, ports: map[int]int{8080: 80}, volumes: map[string]string{"pgadmin4": "/var/lib/pgadmin"}}),
			Entry("phpmyadmin", serviceExpectation{name: "phpmyadmin", repository: "phpmyadmin", containerName: "phpmyadmin", environment: []string{"PMA_ARBITRARY=1"}, ports: map[int]int{8082: 80}}),
			Entry("postgres", serviceExpectation{name: "postgres", repository: "postgres", containerName: "postgres", environment: []string{"POSTGRES_USER=postgres", "POSTGRES_PASSWORD=postgres"}, ports: map[int]int{5432: 5432}, volumes: map[string]string{"postgres": "/var/lib/postgresql/data"}}),
			Entry("redis", serviceExpectation{name: "redis", repository: "redis", containerName: "redis", args: []string{"--save", "60", "1"}, ports: map[int]int{6379: 6379}, volumes: map[string]string{"redis": "/data"}}),
			Entry("redis stack", serviceExpectation{name: "redis-stack", repository: "redis/redis-stack", containerName: "redis-stack", ports: map[int]int{6379: 6379, 8083: 8001}, volumes: map[string]string{"redis-stack": "/data"}}),
		)
	})
})

var _ = Describe("Owned Docker service management", func() {
	var transport *MockRoundTripper
	var store *update.Store
	var installation string
	var definition catalogue.App
	BeforeEach(func() {
		transport = NewMockRoundTripper(gomock.NewController(GinkgoT()))
		base := GinkgoT().TempDir()
		var err error
		store, err = update.NewStore(storage.Paths{Cache: filepath.Join(base, "cache", "idapps", "catalogues"), Config: filepath.Join(base, "config", "idapps"), Bin: filepath.Join(base, "bin", "idapps")}, "0.0.0")
		Expect(err).NotTo(HaveOccurred())
		definition = catalogue.App{Kind: "service", ContainerName: "db", Mounts: []catalogue.Mount{{Type: "volume", Name: "db-data"}}}
		definition = validServiceFixture(definition)
		Expect(store.WithLock(context.Background(), func() error {
			id, records, err := store.Resources()
			if err != nil {
				return err
			}
			installation = id.Installation
			records["db"] = update.ServiceRecord{App: "db", Engine: "engine-1", ContainerID: "owned-container", Definition: definition}
			return store.SaveResources(id, records)
		})).To(Succeed())
		previous := openEngine
		DeferCleanup(func() { openEngine = previous })
		openEngine = func(context.Context) (*client.Client, error) { return dockerClient(transport), nil }
	})
	When("the selected engine differs from the recorded engine", func() {
		It("refuses to touch the recorded service", func() {
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/info")).Return(dockerResponse(http.StatusOK, `{"ID":"engine-2","OSType":"linux"}`), nil)
			err := store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", nil, []string{"stop"}) })
			Expect(err).To(MatchError(ContainSubstring("different Docker engine")))
		})
	})
	When("a retired service is stopped", func() {
		It("stops only the recorded owned container and retains its volume record", func() {
			gomock.InOrder(
				transport.EXPECT().RoundTrip(dockerRequest("GET", "/info")).Return(dockerResponse(http.StatusOK, `{"ID":"engine-1","OSType":"linux"}`), nil),
				transport.EXPECT().RoundTrip(dockerRequest("GET", "/containers/db/json")).DoAndReturn(func(*http.Request) (*http.Response, error) {
					body := fmt.Sprintf(`{"Id":"owned-container","Config":{"Labels":{"io.idapps.installation":%q,"io.idapps.app":"db"}},"State":{"Running":true}}`, installation)
					return dockerResponse(http.StatusOK, body), nil
				}),
				transport.EXPECT().RoundTrip(dockerRequest("POST", "/containers/owned-container/stop")).Return(dockerResponse(http.StatusNoContent, ""), nil),
			)
			Expect(store.WithLock(context.Background(), func() error { return Service(context.Background(), store, "db", nil, []string{"stop"}) })).To(Succeed())
			Expect(store.WithLock(context.Background(), func() error {
				_, records, err := store.Resources()
				if err == nil {
					Expect(records).To(HaveKey("db"))
				}
				return err
			})).To(Succeed())
		})
	})
})

func validServiceFixture(app catalogue.App) catalogue.App {
	if app.Ports == nil {
		app.Ports = []catalogue.Port{}
	}
	if app.Image.Repository == "" {
		app.Image = catalogue.Image{Repository: "postgres", Tag: "1"}
	}
	if app.Network == "" {
		app.Network = "bridge"
	}
	app.User = catalogue.User{Linux: "image", Darwin: "image", Windows: "image"}
	for i := range app.Mounts {
		if app.Mounts[i].Target == "" {
			app.Mounts[i].Target = "/data"
		}
	}
	return app
}
