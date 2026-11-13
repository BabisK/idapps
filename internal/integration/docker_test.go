// Package integration exercises the built launcher against an explicitly selected local Docker engine.
package integration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/BabisK/idapps/internal/testutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/docker/docker/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/BabisK/idapps/internal/engine"
	"github.com/BabisK/idapps/internal/storage"
	"github.com/BabisK/idapps/internal/update"
)

var _ = Describe("A Redis service on a local Docker engine", func() {
	When("the service is stopped and restarted", func() {
		It("retains data and prevents cleanup while running", func() {
			t := GinkgoT()
			if os.Getenv("IDAPPS_DOCKER_INTEGRATION") != "1" {
				t.Skip("set IDAPPS_DOCKER_INTEGRATION=1 for a real local Docker engine")
			}
			if runtime.GOOS != "linux" {
				t.Skip("this integration test targets native Linux runners")
			}
			binary := os.Getenv("IDAPPS_TEST_BINARY")
			Expect(filepath.IsAbs(binary)).To(BeTrue(), "IDAPPS_TEST_BINARY must be the absolute path to a built launcher")
			{

				_, err := os.Stat(binary)
				Expect(err).NotTo(HaveOccurred())
			}

			base := t.TempDir()
			t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
			t.Setenv("DOCKER_CONTEXT", "")
			t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
			t.Setenv("DOCKER_CONFIG", filepath.Join(base, "docker-config"))
			ep, err := engine.SelectEndpoint()
			Expect(err).NotTo(HaveOccurred())

			cli, err := client.NewClientWithOpts(client.WithHost(ep.Host), client.WithAPIVersionNegotiation())
			Expect(err).NotTo(HaveOccurred())

			v, err := cli.ServerVersion(context.Background())
			Expect(err).NotTo(HaveOccurred(), "local engine unavailable: %v", err)
			// Keep even opt-in verification offline: never trigger an image pull.
			_, err = cli.ImageInspect(context.Background(), "redis:7.0.5-alpine3.16")
			cli.Close()
			Expect(err).NotTo(HaveOccurred(), "preload redis:7.0.5-alpine3.16 before running the real-engine check")

			t.Logf("native GOOS=%s GOARCH=%s Docker=%s API=%s engine=%s/%s", runtime.GOOS, runtime.GOARCH, v.Version, v.APIVersion, v.Os, v.Arch)
			var nonce [8]byte
			{
				_, err = rand.Read(nonce[:])
				Expect(err).NotTo(HaveOccurred())
			}

			resource := "idappstest" + hex.EncodeToString(nonce[:])
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			Expect(err).NotTo(HaveOccurred())

			port := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			files, err := testutil.CatalogueFiles("../../catalogue")
			Expect(err).NotTo(HaveOccurred())
			var server map[string]any
			Expect(json.Unmarshal(files["repo/catalogue/redis/app.json"], &server)).To(Succeed())
			server["containerName"] = resource
			for _, entry := range server["mounts"].([]any) {
				m := entry.(map[string]any)
				if m["type"] == "volume" {
					m["name"] = resource
				}
			}
			binding := server["ports"].([]any)[0].(map[string]any)
			binding["hostIP"] = "127.0.0.1"
			binding["hostPort"] = port
			redisData, err := json.Marshal(server)
			Expect(err).NotTo(HaveOccurred())
			for name := range files {
				if strings.HasPrefix(name, "repo/catalogue/") && strings.HasSuffix(name, "/app.json") && name != "repo/catalogue/redis/app.json" && name != "repo/catalogue/redis-cli/app.json" {
					delete(files, name)
				}
			}
			files["repo/catalogue/redis/app.json"] = redisData
			b, err := testutil.CatalogueArchive(files)
			Expect(err).NotTo(HaveOccurred())

			digest := sha256.Sum256(b)
			generation := &update.Generation{Commit: strings.Repeat("a", 40), SHA256: hex.EncodeToString(digest[:]), Bytes: b}
			paths, err := storage.Resolve()
			Expect(err).NotTo(HaveOccurred())

			store, err := update.NewStore(paths, "0.0.0")
			Expect(err).NotTo(HaveOccurred())

			err = store.WithLock(context.Background(), func() error { return store.Activate(generation, binary) })
			Expect(err).NotTo(HaveOccurred())

			run := func(exe string, args ...string) (string, error) {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				c := exec.CommandContext(ctx, exe, args...)
				c.Dir = base
				out, err := c.CombinedOutput()
				if ctx.Err() != nil {
					return string(out), ctx.Err()
				}
				return string(out), err
			}
			redis := func(args ...string) (string, error) { return run(binary, append([]string{"redis"}, args...)...) }
			clientArgs := []string{"-h", "127.0.0.1", "-p", fmt.Sprint(port)}
			redisClient := func(exe string, prefix []string, args ...string) (string, error) {
				all := append(append(append([]string{}, prefix...), clientArgs...), args...)
				return run(exe, all...)
			}
			attempted := false
			t.Cleanup(func() {
				if !attempted {
					return
				}
				_, _ = redis("stop")
				var recorded bool
				e := store.WithLock(context.Background(), func() error {
					_, records, e := store.Resources()
					if e == nil {
						_, recorded = records["redis"]
					}
					return e
				})
				if e != nil {
					t.Errorf("inspect cleanup record: %v", e)
					return
				}
				if recorded {
					out, e := redis("clean")
					if e != nil {
						t.Errorf("test resource %s could not be cleaned: %v: %s", resource, e, out)
					}
				}
			})
			By("starting the service and waiting for clients")
			attempted = true
			{
				out, e := redis("start")
				Expect(e).NotTo(HaveOccurred(), "start: %v: %s", e, out)
			}

			ready := func() {
				t.Helper()
				deadline := time.Now().Add(60 * time.Second)
				for time.Now().Before(deadline) {
					out, e := redisClient(binary, []string{"redis-cli"}, "PING")
					if e == nil && strings.Contains(out, "PONG") {
						return
					}
					time.Sleep(time.Second)
				}
				t.Fatal("redis client did not connect before timeout")
			}
			ready()
			By("writing data through the installed client link")
			link := filepath.Join(paths.Bin, "redis-cli")
			{
				out, e := redisClient(link, nil, "SET", "idapps-ci", "retained")
				Expect(e).NotTo(HaveOccurred(), "linked client SET: %s", out)
				Expect(out).To(ContainSubstring("OK"))
			}
			By("refusing to clean a running service")
			{

				out, e := redis("clean")
				Expect(e).To(HaveOccurred(), "clean succeeded while running: %s", out)
			}
			{

				out, e := redis("logs", "-t", "2")
				Expect(e).NotTo(HaveOccurred(), "logs: %v: %s", e, out)
			}
			{

				out, e := redis("stop")
				Expect(e).NotTo(HaveOccurred(), "stop: %v: %s", e, out)
			}
			{

				out, e := redis("start")
				Expect(e).NotTo(HaveOccurred(), "restart: %v: %s", e, out)
			}

			By("reading the data after restarting the service")
			ready()
			{
				out, e := redisClient(binary, []string{"run", "redis-cli"}, "GET", "idapps-ci")
				Expect(e).NotTo(HaveOccurred(), "persisted GET: %s", out)
				Expect(strings.TrimSpace(out)).To(Equal("retained"))
			}

		})
	})
})
