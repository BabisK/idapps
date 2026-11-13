package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/BabisK/idapps/internal/catalogue"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var _ = Describe("running a Docker-backed command", func() {
	It("preserves command arguments, streams and exit status", func() {
		t := GinkgoT()
		oldFactory := openEngine
		defer func() { openEngine = oldFactory }()
		oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
		defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()
		in, e := os.Open(os.DevNull)
		Expect(e).NotTo(HaveOccurred())

		defer in.Close()
		out, e := os.CreateTemp(t.TempDir(), "out")
		Expect(e).NotTo(HaveOccurred())

		defer out.Close()
		errout, e := os.CreateTemp(t.TempDir(), "err")
		Expect(e).NotTo(HaveOccurred())

		defer errout.Close()
		os.Stdin, os.Stdout, os.Stderr = in, out, errout
		var created chan []string = make(chan []string, 1)
		dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				req, err := http.ReadRequest(bufio.NewReader(serverConn))
				if err != nil {
					return
				}
				defer req.Body.Close()
				body, _ := io.ReadAll(req.Body)
				path := req.URL.Path
				if strings.Contains(path, "/attach") {
					fmt.Fprint(serverConn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Type: application/vnd.docker.raw-stream\r\n\r\n")
					if req.URL.Query().Get("stdin") == "1" {
						_, _ = io.Copy(io.Discard, serverConn)
						return
					}
					for _, f := range []struct {
						stream  byte
						payload string
					}{{1, "out\x00"}, {2, "err\xff"}} {
						header := make([]byte, 8)
						header[0] = f.stream
						binary.BigEndian.PutUint32(header[4:], uint32(len(f.payload)))
						_, _ = serverConn.Write(header)
						_, _ = io.WriteString(serverConn, f.payload)
					}
					return
				}
				response := "{}"
				status := "200 OK"
				switch {
				case strings.HasSuffix(path, "/containers/create"):
					var data struct{ Cmd []string }
					_ = json.Unmarshal(body, &data)
					created <- data.Cmd
					response = `{"Id":"abcdef123456","Warnings":[]}`
				case strings.HasSuffix(path, "/wait"):
					response = `{"StatusCode":7}`
				case strings.HasSuffix(path, "/start"), req.Method == "DELETE":
					status = "204 No Content"
					response = ""
				}
				fmt.Fprintf(serverConn, "HTTP/1.1 %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", status, len(response), response)
			}()
			return clientConn, nil
		}
		openEngine = func(context.Context) (*client.Client, error) {
			transport := &http.Transport{DialContext: dial}
			return client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: transport}))
		}
		app := catalogue.App{Kind: "command", Image: catalogue.Image{Repository: "image", Tag: "1"}, Network: "host", Args: []string{"tool"}}
		code, e := RunCommand(context.Background(), app, []string{"", "a b", "$value"})
		Expect(e).NotTo(HaveOccurred())
		Expect(code).To(Equal(7))

		cmd := <-created
		Expect(cmd).To(Equal([]string{"tool", "", "a b", "$value"}))

		ob, _ := os.ReadFile(out.Name())
		eb, _ := os.ReadFile(errout.Name())
		Expect(string(ob)).To(Equal("out\x00"))
		Expect(string(eb)).To(Equal("err\xff"))

	})
})

var _ = Describe("command streams", func() {
	var (
		stdoutBytes, stderrBytes, stdinBytes []byte
		stdinSeen                            chan []byte
		events                               chan string
		outputFile, errorFile                *os.File
	)
	BeforeEach(func() {
		stdoutBytes = bytes.Repeat([]byte{0, 1, 0x7f, 0x80, 0xff, '\n'}, 65536)
		stderrBytes = bytes.Repeat([]byte{'e', 0, 0xfe, '\r', '\n'}, 65536)
		stdinBytes = []byte{0, 1, 0xff, '\n', 'e', 'o', 'f'}
		stdinSeen = make(chan []byte, 1)
		events = make(chan string, 100)
		inputChannel, trace, outData, errData := stdinSeen, events, stdoutBytes, stderrBytes
		inputDone := make(chan struct{})
		var once sync.Once
		DeferCleanup(func() { once.Do(func() { close(inputDone) }) })
		oldIn, oldOut, oldErr, oldFactory := os.Stdin, os.Stdout, os.Stderr, openEngine
		DeferCleanup(func() { os.Stdin, os.Stdout, os.Stderr, openEngine = oldIn, oldOut, oldErr, oldFactory })
		dir := GinkgoT().TempDir()
		inputPath := filepath.Join(dir, "stdin")
		Expect(os.WriteFile(inputPath, stdinBytes, 0600)).To(Succeed())
		input, err := os.Open(inputPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = input.Close() })
		outputFile, err = os.CreateTemp(dir, "stdout")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = outputFile.Close() })
		errorFile, err = os.CreateTemp(dir, "stderr")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = errorFile.Close() })
		os.Stdin, os.Stdout, os.Stderr = input, outputFile, errorFile
		dial := func(ctx context.Context, network, address string) (net.Conn, error) {
			local, remote := net.Pipe()
			go func() {
				defer remote.Close()
				request, err := http.ReadRequest(bufio.NewReader(remote))
				if err != nil {
					return
				}
				defer request.Body.Close()
				_, _ = io.Copy(io.Discard, request.Body)
				path := request.URL.Path
				trace <- request.Method + " " + path + "?" + request.URL.RawQuery
				if strings.HasSuffix(path, "/attach") {
					_, _ = fmt.Fprint(remote, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Type: application/vnd.docker.raw-stream\r\n\r\n")
					if request.URL.Query().Get("stdin") == "1" {
						data, _ := io.ReadAll(remote)
						inputChannel <- data
						trace <- fmt.Sprintf("stdin received %d", len(data))
						once.Do(func() { close(inputDone) })
						return
					}
					<-inputDone
					for _, frame := range []struct {
						channel byte
						data    []byte
					}{{1, outData}, {2, errData}} {
						header := make([]byte, 8)
						header[0] = frame.channel
						binary.BigEndian.PutUint32(header[4:], uint32(len(frame.data)))
						if _, err := remote.Write(header); err != nil {
							return
						}
						if _, err := remote.Write(frame.data); err != nil {
							return
						}
					}
					return
				}
				status, body := "200 OK", "{}"
				switch {
				case strings.HasSuffix(path, "/containers/create"):
					body = `{"Id":"owned-io-command"}`
				case strings.HasSuffix(path, "/wait"):
					_, _ = fmt.Fprint(remote, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n")
					<-inputDone
					body = `{"StatusCode":17}`
					_, _ = fmt.Fprintf(remote, "%x\r\n%s\r\n0\r\n\r\n", len(body), body)
					return
				case strings.HasSuffix(path, "/start"), request.Method == "DELETE":
					status, body = "204 No Content", ""
				}
				_, _ = fmt.Fprintf(remote, "HTTP/1.1 %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", status, len(body), body)
			}()
			return local, nil
		}
		openEngine = func(context.Context) (*client.Client, error) {
			return client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: &http.Transport{DialContext: dial}}))
		}
	})
	When("stdin reaches EOF after binary input", func() {
		It("forwards EOF and preserves large separate binary streams and exit status", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			code, err := RunCommand(ctx, catalogue.App{Kind: "command", Image: catalogue.Image{Repository: "image", Tag: "1"}, Network: "host"}, nil)
			if err != nil {
				var trace []string
				for len(events) > 0 {
					trace = append(trace, <-events)
				}
				Fail(fmt.Sprintf("%v; events: %v", err, trace))
			}
			Expect(code).To(Equal(17))
			Eventually(stdinSeen, time.Second).Should(Receive(Equal(stdinBytes)))
			out, err := os.ReadFile(outputFile.Name())
			Expect(err).NotTo(HaveOccurred())
			errout, err := os.ReadFile(errorFile.Name())
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal(stdoutBytes))
			Expect(errout).To(Equal(stderrBytes))
		})
	})
	When("the downstream stdout reader closes", func() {
		It("returns a stream failure and does not hang", func() {
			readEnd, writeEnd, err := os.Pipe()
			Expect(err).NotTo(HaveOccurred())
			Expect(readEnd.Close()).To(Succeed())
			DeferCleanup(func() { _ = writeEnd.Close() })
			os.Stdout = writeEnd
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			code, err := RunCommand(ctx, catalogue.App{Kind: "command", Image: catalogue.Image{Repository: "image", Tag: "1"}, Network: "host"}, nil)
			Expect(err).To(HaveOccurred())
			Expect(err).NotTo(MatchError(context.DeadlineExceeded))
			Expect(code).To(Equal(125))
			Eventually(stdinSeen, time.Second).Should(Receive(Equal(stdinBytes)))
		})
	})
})

type commandExpectation struct {
	name, repository, workdir, cwdTarget string
	entrypoint, prefix                   []string
	homeMounts                           map[string]string
	environment                          []string
}

var _ = Describe("Catalogue command Docker requests", func() {
	var apps map[string]catalogue.App
	var mock *MockRoundTripper
	var cwd, home string
	BeforeEach(func() {
		parsed, err := catalogue.ReadTree("../../catalogue", "0.0.0")
		Expect(err).NotTo(HaveOccurred())
		apps = parsed.Apps

		base := GinkgoT().TempDir()
		cwd, home = filepath.Join(base, "working directory Ω"), filepath.Join(base, "home Ω")
		Expect(os.MkdirAll(cwd, 0700)).To(Succeed())
		Expect(os.MkdirAll(home, 0700)).To(Succeed())
		GinkgoT().Setenv("HOME", home)
		if runtime.GOOS == "windows" {
			GinkgoT().Setenv("USERPROFILE", home)
		}
		previousDir, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Chdir(cwd)).To(Succeed())
		DeferCleanup(func() { Expect(os.Chdir(previousDir)).To(Succeed()) })
		cwd, err = os.Getwd()
		Expect(err).NotTo(HaveOccurred())

		mock = NewMockRoundTripper(gomock.NewController(GinkgoT()))
		previous := openEngine
		DeferCleanup(func() { openEngine = previous })
		openEngine = func(context.Context) (*client.Client, error) { return dockerClient(mock), nil }
	})

	When("a command is prepared for execution", func() {
		DescribeTable("sends the approved image, arguments, network, user and mounts", func(want commandExpectation) {
			app := apps[want.name]
			Expect(app.Kind).To(Equal("command"))
			Expect(app.Image.Repository).To(Equal(want.repository))
			mock.EXPECT().RoundTrip(gomock.Cond(func(req *http.Request) bool {
				return req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/images/") && strings.HasSuffix(req.URL.Path, "/json")
			})).Return(dockerResponse(http.StatusOK, `{}`), nil)
			var request struct {
				Image      string               `json:"Image"`
				Cmd        []string             `json:"Cmd"`
				Entrypoint []string             `json:"Entrypoint"`
				WorkingDir string               `json:"WorkingDir"`
				User       string               `json:"User"`
				Env        []string             `json:"Env"`
				HostConfig container.HostConfig `json:"HostConfig"`
			}
			mock.EXPECT().RoundTrip(dockerRequest(http.MethodPost, "/containers/create")).DoAndReturn(func(req *http.Request) (*http.Response, error) {
				data, err := io.ReadAll(req.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(json.Unmarshal(data, &request)).To(Succeed())
				// Stop before attach: this scenario examines the create request only.
				return dockerResponse(http.StatusInternalServerError, `{"message":"test stops after create"}`), nil
			})
			args := []string{"", "a b", "--help", "Ω"}
			code, err := RunCommand(context.Background(), app, args)
			Expect(code).To(Equal(125))
			Expect(err).To(MatchError(ContainSubstring("test stops after create")))
			Expect(request.Image).To(Equal(app.Image.Reference()))
			Expect(request.Cmd).To(Equal(append(append([]string{}, want.prefix...), args...)))
			Expect(request.Entrypoint).To(Equal(want.entrypoint))
			Expect(request.WorkingDir).To(Equal(want.workdir))
			Expect(string(request.HostConfig.NetworkMode)).To(Equal("host"))
			Expect(request.HostConfig.AutoRemove).To(BeTrue())
			Expect(request.Env).To(ConsistOf(want.environment))
			if runtime.GOOS == "windows" {
				Expect(request.User).To(BeEmpty())
			} else {
				Expect(request.User).To(Equal(fmtHostUser()))
			}
			wantMounts := map[string]string{}
			if want.cwdTarget != "" {
				wantMounts[want.cwdTarget] = cwd
			}
			for target, source := range want.homeMounts {
				wantMounts[target] = filepath.Join(home, filepath.FromSlash(source))
			}
			Expect(request.HostConfig.Mounts).To(HaveLen(len(wantMounts)))
			for _, binding := range request.HostConfig.Mounts {
				Expect(binding.Type).To(Equal(mount.TypeBind))
				Expect(binding.Source).To(Equal(wantMounts[binding.Target]), "mount %s", binding.Target)
				Expect(binding.ReadOnly).To(BeFalse())
				_, err := os.Stat(binding.Source)
				Expect(err).NotTo(HaveOccurred())
			}
		},
			Entry("AWS", commandExpectation{name: "aws", repository: "amazon/aws-cli", cwdTarget: "/aws", homeMounts: map[string]string{"/tmp/.aws": ".aws"}, environment: []string{"AWS_CONFIG_FILE=/tmp/.aws/config", "AWS_SHARED_CREDENTIALS_FILE=/tmp/.aws/credentials"}}),
			Entry("etcdctl", commandExpectation{name: "etcdctl", repository: "gcr.io/etcd-development/etcd", workdir: "/etcd", cwdTarget: "/etcd", entrypoint: []string{"/usr/local/bin/etcdctl"}}),
			Entry("go", commandExpectation{name: "go", repository: "golang", workdir: "/src", cwdTarget: "/src", prefix: []string{"go"}}),
			Entry("helm", commandExpectation{name: "helm", repository: "alpine/helm", workdir: "/helm", cwdTarget: "/helm", homeMounts: map[string]string{"/.kube": ".kube", "/.helm": ".helm", "/.config/helm": ".config/helm", "/.cache/helm": ".cache/helm"}}),
			Entry("helmfile", commandExpectation{name: "helmfile", repository: "ghcr.io/helmfile/helmfile", workdir: "/helmfile", cwdTarget: "/helmfile", entrypoint: []string{"/usr/local/bin/helmfile"}, homeMounts: map[string]string{"/.kube": ".kube", "/.helm": ".helm", "/.config/helm": ".config/helm", "/.cache/helm": ".cache/helm"}}),
			Entry("kubectl", commandExpectation{name: "kubectl", repository: "alpine/kubectl", homeMounts: map[string]string{"/.kube": ".kube"}, environment: []string{"KUBECONFIG=/.kube/config"}}),
			Entry("mysql", commandExpectation{name: "mysql", repository: "mysql", workdir: "/mysql", cwdTarget: "/mysql", prefix: []string{"mysql"}}),
			Entry("mysqldump", commandExpectation{name: "mysqldump", repository: "mysql", workdir: "/mysql", cwdTarget: "/mysql", prefix: []string{"mysqldump"}}),
			Entry("nats", commandExpectation{name: "nats", repository: "natsio/nats-box", workdir: "/nats", cwdTarget: "/nats", entrypoint: []string{"/usr/local/bin/nats"}}),
			Entry("pg_dump", commandExpectation{name: "pg_dump", repository: "postgres", workdir: "/postgres", cwdTarget: "/postgres", prefix: []string{"pg_dump"}}),
			Entry("psql", commandExpectation{name: "psql", repository: "postgres", workdir: "/postgres", cwdTarget: "/postgres", prefix: []string{"psql"}}),
			Entry("redis-cli", commandExpectation{name: "redis-cli", repository: "redis", workdir: "/redis", cwdTarget: "/redis", prefix: []string{"redis-cli"}}),
		)
	})
})

func fmtHostUser() string { return fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()) }

var _ = Describe("Command bind mounts", func() {
	var home string
	BeforeEach(func() {
		home = filepath.Join(GinkgoT().TempDir(), "home with spaces Ω")
		Expect(os.MkdirAll(home, 0700)).To(Succeed())
		GinkgoT().Setenv("HOME", home)
		if runtime.GOOS == "windows" {
			GinkgoT().Setenv("USERPROFILE", home)
		}
	})
	When("a catalogue requests a missing home directory", func() {
		It("creates it when creation is enabled", func() {
			app := catalogue.App{Mounts: []catalogue.Mount{{Type: "bind", Source: catalogue.Source{Base: "home", Path: ".config/tool"}, Target: "/config", Create: true, ReadOnly: true}}}
			mounts, err := bindSources(app)
			Expect(err).NotTo(HaveOccurred())
			Expect(mounts).To(Equal([]mount.Mount{{Type: mount.TypeBind, Source: filepath.Join(home, ".config", "tool"), Target: "/config", ReadOnly: true}}))
			info, err := os.Stat(filepath.Join(home, ".config", "tool"))
			Expect(err).NotTo(HaveOccurred())
			Expect(info.IsDir()).To(BeTrue())
		})
		It("fails without creating it when creation is disabled", func() {
			path := filepath.Join(home, "missing")
			app := catalogue.App{Mounts: []catalogue.Mount{{Type: "bind", Source: catalogue.Source{Base: "home", Path: "missing"}, Target: "/config"}}}
			_, err := bindSources(app)
			Expect(err).To(MatchError(ContainSubstring("not an accessible directory")))
			_, err = os.Lstat(path)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})
	})
	When("the requested mount source is not an acceptable directory", func() {
		It("rejects regular files", func() {
			Expect(os.WriteFile(filepath.Join(home, "config"), []byte("data"), 0600)).To(Succeed())
			app := catalogue.App{Mounts: []catalogue.Mount{{Type: "bind", Source: catalogue.Source{Base: "home", Path: "config"}, Target: "/config"}}}
			_, err := bindSources(app)
			Expect(err).To(MatchError(ContainSubstring("not an accessible directory")))
		})
		It("rejects links resolving to a Docker socket named directory", func() {
			target := filepath.Join(home, "docker.sock")
			Expect(os.Mkdir(target, 0700)).To(Succeed())
			if err := os.Symlink(target, filepath.Join(home, "link")); err != nil {
				Skip("symbolic-link creation is unavailable: " + err.Error())
			}
			app := catalogue.App{Mounts: []catalogue.Mount{{Type: "bind", Source: catalogue.Source{Base: "home", Path: "link"}, Target: "/config"}}}
			_, err := bindSources(app)
			Expect(err).To(MatchError(ContainSubstring("Docker socket mounts are forbidden")))
		})
		It("refuses a volume on a command", func() {
			_, err := bindSources(catalogue.App{Mounts: []catalogue.Mount{{Type: "volume", Name: "data", Target: "/data"}}})
			Expect(err).To(MatchError(ContainSubstring("unexpected volume")))
		})
	})
})

var _ = Describe("running a Docker-backed command", func() {
	Context("when execution is cancelled", func() {
		DescribeTable("interrupts and removes only its own container on cancellation", func(holdStartResponse bool) {
			t := GinkgoT()
			old := openEngine
			defer func() { openEngine = old }()
			oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
			defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()
			in, err := os.Open(os.DevNull)
			Expect(err).NotTo(HaveOccurred())

			defer in.Close()
			out, err := os.CreateTemp(t.TempDir(), "stdout")
			Expect(err).NotTo(HaveOccurred())

			defer out.Close()
			errout, err := os.CreateTemp(t.TempDir(), "stderr")
			Expect(err).NotTo(HaveOccurred())

			defer errout.Close()
			os.Stdin, os.Stdout, os.Stderr = in, out, errout
			started := make(chan struct{})
			events := make(chan string, 20)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			dial := func(_ context.Context, network, addr string) (net.Conn, error) {
				local, remote := net.Pipe()
				go func() {
					defer remote.Close()
					r, e := http.ReadRequest(bufio.NewReader(remote))
					if e != nil {
						return
					}
					defer r.Body.Close()
					_, _ = io.Copy(io.Discard, r.Body)
					p := r.URL.Path
					events <- r.Method + " " + p + "?" + r.URL.RawQuery
					if strings.HasSuffix(p, "/attach") {
						_, _ = fmt.Fprint(remote, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Type: application/vnd.docker.raw-stream\r\n\r\n")
						_, _ = io.Copy(io.Discard, remote)
						if !holdStartResponse && r.URL.Query().Get("stdin") == "1" && r.URL.Query().Get("stdout") != "1" {
							close(started)
						}
						return
					}
					if strings.HasSuffix(p, "/wait") {
						_, _ = fmt.Fprint(remote, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n")
						_, _ = io.Copy(io.Discard, remote)
						return
					}
					status, body := "200 OK", "{}"
					switch {
					case strings.HasSuffix(p, "/containers/create"):
						body = `{"Id":"owned-command"}`
					case strings.HasSuffix(p, "/start"):
						if holdStartResponse {
							close(started)
							<-ctx.Done()
							return
						}
						status = "204 No Content"
						body = ""
					case strings.HasSuffix(p, "/kill"), r.Method == "DELETE":
						status = "204 No Content"
						body = ""
					}
					_, _ = fmt.Fprintf(remote, "HTTP/1.1 %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", status, len(body), body)
				}()
				return local, nil
			}
			openEngine = func(context.Context) (*client.Client, error) {
				return client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: &http.Transport{DialContext: dial}}))
			}
			type result struct {
				code int
				err  error
			}
			done := make(chan result, 1)
			go func() {
				code, e := RunCommand(ctx, catalogue.App{Kind: "command", Image: catalogue.Image{Repository: "image", Tag: "1"}, Network: "host"}, nil)
				done <- result{code, e}
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("command did not start")
			}
			cancel()
			select {
			case r := <-done:
				Expect(r.code != 130 || !errors.Is(r.err, context.Canceled)).To(BeFalse(), "got exit %d, error %v", r.code, r.err)

			case <-time.After(5 * time.Second):
				t.Fatal("canceled command did not return")
			}
			var kill, remove, pull bool
			for len(events) > 0 {
				e := <-events
				if strings.Contains(e, "/containers/owned-command/kill?") && strings.Contains(e, "signal=SIGINT") {
					kill = true
				}
				if strings.HasPrefix(e, "DELETE ") && strings.Contains(e, "/containers/owned-command?") {
					remove = true
				}
				if strings.Contains(e, "/images/create") {
					pull = true
				}
				Expect(strings.HasPrefix(e, "DELETE ") && !strings.Contains(e, "/containers/owned-command?")).To(BeFalse(), "unexpected removal: %s", e)

			}
			Expect(!kill || !remove || pull).To(BeFalse(), "interrupt=%v remove=%v unexpected pull=%v", kill, remove, pull)

		},
			Entry("after the start response", false),
			Entry("while the start response is pending", true),
		)
		It("returns cancellation when image inspection is interrupted", func() {

			old := openEngine
			defer func() { openEngine = old }()
			openEngine = func(context.Context) (*client.Client, error) {
				rt := serviceTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
				return client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: rt}))
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			code, err := RunCommand(ctx, catalogue.App{Image: catalogue.Image{Repository: "image", Tag: "1"}}, nil)
			Expect(code != 130 || !errors.Is(err, context.Canceled)).To(BeFalse(), "got exit %d and error %v", code, err)

		})
	})
	Context("when the image cannot be pulled", func() {
		It("does not create a container when anonymous image pulling fails", func() {

			old := openEngine
			defer func() { openEngine = old }()
			var requests []string
			rt := serviceTransport(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				status, body := 200, `{}`
				switch {
				case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
					status = 404
					body = `{"message":"missing image"}`
				case strings.HasSuffix(r.URL.Path, "/images/create"):
					body = "{\"errorDetail\":{\"message\":\"authentication required\"},\"error\":\"authentication required\"}\n"
				default:
					return nil, fmt.Errorf("unexpected request %s", r.URL.Path)
				}
				return &http.Response{StatusCode: status, Status: fmt.Sprint(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			openEngine = func(context.Context) (*client.Client, error) {
				return client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: rt}))
			}
			code, err := RunCommand(context.Background(), catalogue.App{Image: catalogue.Image{Repository: "image", Tag: "1"}}, nil)
			Expect(code != 125 || err == nil || !strings.Contains(err.Error(), "authentication required")).To(BeFalse(), "got %d, %v", code, err)
			Expect(len(requests) != 2 || !strings.Contains(requests[1], "/images/create")).To(BeFalse(), "unexpected engine requests: %v", requests)

		})
	})
	Context("when checking the local engine", func() {
		DescribeTable("reports an unusable engine", func(status int, response, expected string) {
			var paths []string
			rt := serviceTransport(func(r *http.Request) (*http.Response, error) {
				paths = append(paths, r.URL.Path)
				return &http.Response{StatusCode: status, Status: fmt.Sprint(status), Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header), Request: r}, nil
			})
			cli, err := client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: rt}))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(cli.Close)
			err = checkEngine(context.Background(), cli, Endpoint{Host: "unix:///var/run/docker.sock"})
			Expect(err).To(MatchError(ContainSubstring(expected)))
			Expect(paths).To(HaveLen(1))
			Expect(paths[0]).To(HaveSuffix("/info"))
		},
			Entry("access is denied", 403, `{"message":"permission denied"}`, "check permissions"),
			Entry("Windows containers are enabled", 200, `{"OSType":"windows"}`, "not using Linux containers"),
		)
	})
})

//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -destination mock_roundtripper_test.go -package engine net/http RoundTripper

func dockerRequest(method, suffix string) gomock.Matcher {
	return gomock.Cond(func(r *http.Request) bool { return r.Method == method && strings.HasSuffix(r.URL.Path, suffix) })
}

func dockerResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}}
}

func dockerClient(transport http.RoundTripper) *client.Client {
	cli, err := client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: transport}))
	Expect(err).NotTo(HaveOccurred())
	return cli
}

var _ = Describe("cleanup of a command container", func() {
	var transport *MockRoundTripper
	var cli *client.Client
	BeforeEach(func() {
		transport = NewMockRoundTripper(gomock.NewController(GinkgoT()))
		cli = dockerClient(transport)
		DeferCleanup(cli.Close)
	})
	When("Docker is already removing the container", func() {
		It("waits until the container is gone", func() {
			gomock.InOrder(
				transport.EXPECT().RoundTrip(dockerRequest("DELETE", "/containers/owned")).Return(dockerResponse(http.StatusConflict, `{"message":"removal of container owned is already in progress"}`), nil),
				transport.EXPECT().RoundTrip(dockerRequest("DELETE", "/containers/owned")).Return(dockerResponse(http.StatusNotFound, `{"message":"No such container"}`), nil),
			)
			Expect(removeCommandContainer(context.Background(), cli, "owned")).To(Succeed())
		})
		It("reports when removal does not finish before the deadline", func() {
			transport.EXPECT().RoundTrip(dockerRequest("DELETE", "/containers/owned")).Return(dockerResponse(http.StatusConflict, `{"message":"removal of container owned is already in progress"}`), nil).AnyTimes()
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			Expect(removeCommandContainer(ctx, cli, "owned")).To(MatchError(ContainSubstring("container removal still in progress")))
		})
	})
	When("Docker reports a different conflict", func() {
		It("returns the error without retrying", func() {
			transport.EXPECT().RoundTrip(dockerRequest("DELETE", "/containers/owned")).Return(dockerResponse(http.StatusConflict, `{"message":"another conflict"}`), nil)
			Expect(removeCommandContainer(context.Background(), cli, "owned")).To(MatchError(ContainSubstring("another conflict")))
		})
	})
})

var _ = Describe("Docker API compatibility", func() {
	DescribeTable("checks the negotiated API before allowing engine use", func(version string, supported bool) {
		transport := NewMockRoundTripper(gomock.NewController(GinkgoT()))
		transport.EXPECT().RoundTrip(dockerRequest("GET", "/info")).Return(dockerResponse(http.StatusOK, `{"OSType":"linux"}`), nil)
		cli, err := client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion(version), client.WithHTTPClient(&http.Client{Transport: transport}))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cli.Close)
		err = checkEngine(context.Background(), cli, Endpoint{Host: "unix:///docker.sock"})
		if supported {
			Expect(err).NotTo(HaveOccurred())
		} else {
			Expect(err).To(MatchError(ContainSubstring("API 1.30 or newer")))
		}
	},
		Entry("rejects an older engine", "1.29", false),
		Entry("accepts the minimum version", "1.30", true),
	)
})

var _ = Describe("Docker image availability", func() {
	var transport *MockRoundTripper
	var cli *client.Client
	BeforeEach(func() {
		transport = NewMockRoundTripper(gomock.NewController(GinkgoT()))
		cli = dockerClient(transport)
		DeferCleanup(cli.Close)
	})
	When("the image is already cached", func() {
		It("uses it without pulling", func() {
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/images/redis:7/json")).Return(dockerResponse(http.StatusOK, `{}`), nil)
			Expect(ensureImage(context.Background(), cli, "redis:7")).To(Succeed())
		})
	})
	When("the image is missing", func() {
		BeforeEach(func() {
			transport.EXPECT().RoundTrip(dockerRequest("GET", "/images/redis:7/json")).Return(dockerResponse(http.StatusNotFound, `{"message":"not found"}`), nil)
		})
		It("pulls anonymously and reports success", func() {
			transport.EXPECT().RoundTrip(dockerRequest("POST", "/images/create")).DoAndReturn(func(req *http.Request) (*http.Response, error) {
				Expect(req.URL.Query().Get("fromImage")).To(Equal("docker.io/library/redis"))
				Expect(req.URL.Query().Get("tag")).To(Equal("7"))
				Expect(req.Header.Get("X-Registry-Auth")).To(BeEmpty())
				Expect(req.Header.Get("Authorization")).To(BeEmpty())
				return dockerResponse(http.StatusOK, "{\"status\":\"downloaded\"}\n"), nil
			})
			Expect(ensureImage(context.Background(), cli, "redis:7")).To(Succeed())
		})
		It("reports an authentication-required pull failure", func() {
			transport.EXPECT().RoundTrip(dockerRequest("POST", "/images/create")).Return(dockerResponse(http.StatusUnauthorized, `{"message":"authentication required"}`), nil)
			Expect(ensureImage(context.Background(), cli, "redis:7")).To(MatchError(ContainSubstring("authentication required")))
		})
		It("reports an error sent in the pull stream", func() {
			transport.EXPECT().RoundTrip(dockerRequest("POST", "/images/create")).Return(dockerResponse(http.StatusOK, "{\"errorDetail\":{\"message\":\"denied\"}}\n"), nil)
			Expect(ensureImage(context.Background(), cli, "redis:7")).To(MatchError(ContainSubstring("denied")))
		})
		It("reports malformed pull progress", func() {
			transport.EXPECT().RoundTrip(dockerRequest("POST", "/images/create")).Return(dockerResponse(http.StatusOK, "{invalid"), nil)
			Expect(ensureImage(context.Background(), cli, "redis:7")).To(MatchError(ContainSubstring("read image pull")))
		})
	})
})

var _ = Describe("image pull progress", func() {
	When("Docker sends repeated updates for multiple layers", func() {
		It("reports each layer's status changes without repeating progress events", func() {
			stream := strings.Join([]string{
				`{"status":"Downloading","id":"layer-a"}`,
				`{"status":"Downloading","id":"layer-b"}`,
				`{"status":"Downloading","id":"layer-a","progressDetail":{"current":10}}`,
				`{"status":"Extracting","id":"layer-a"}`,
				`{"status":"Downloading","id":"layer-b","progressDetail":{"current":20}}`,
				`{"status":"Pull complete","id":"layer-a"}`,
				`{"status":"Status: Downloaded newer image"}`,
			}, "\n")
			var output bytes.Buffer
			Expect(reportPullProgress(strings.NewReader(stream), &output, "example:1")).To(Succeed())
			Expect(output.String()).To(Equal("layer-a: Downloading\nlayer-b: Downloading\nlayer-a: Extracting\nlayer-a: Pull complete\nStatus: Downloaded newer image\n"))
		})
	})
})
