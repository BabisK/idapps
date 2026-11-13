package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/BabisK/idapps/internal/catalogue"
	"github.com/BabisK/idapps/internal/storage"
	"github.com/BabisK/idapps/internal/testutil"
	"github.com/BabisK/idapps/internal/update"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"
)

func run(argv []string) int { return Execute("1.0.0", argv) }

type catalogueTransport func(*http.Request) (*http.Response, error)

func (f catalogueTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

var _ = Describe("The launcher", func() {
	BeforeEach(func() {
		if runtime.GOOS != "linux" {
			Skip("isolated XDG test")
		}
		t := GinkgoT()
		base := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
		t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	})

	When("no catalogue has been downloaded", func() {
		DescribeTable("returns the appropriate status", func(args []string, status int) {
			Expect(run(args)).To(Equal(status))
		},
			Entry("help remains available", []string{"idapps", "help"}, 0),
			Entry("version remains available", []string{"idapps", "version"}, 0),
			Entry("listing apps requires an update", []string{"idapps", "list"}, 125),
			Entry("running an app requires an update", []string{"idapps", "psql", "--help"}, 125),
			Entry("run without an app is invalid", []string{"idapps", "run"}, 2),
		)
	})
	When("the catalogue is updated", func() {
		It("installs and repairs links using only mocked HTTP responses", func() {
			const commit = "0123456789abcdef0123456789abcdef01234567"
			var archive bytes.Buffer
			zw := zip.NewWriter(&archive)
			files := map[string]string{}
			err := filepath.WalkDir("../catalogue", func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if p == "../catalogue" {
					return nil
				}
				rel, err := filepath.Rel("../catalogue", p)
				if err != nil {
					return err
				}
				rel = filepath.ToSlash(rel)
				if !d.IsDir() {
					b, e := os.ReadFile(p)
					if e != nil {
						return e
					}
					files[rel] = string(b)
					f, e := zw.Create("repo/catalogue/" + rel)
					if e != nil {
						return e
					}
					_, e = f.Write(b)
					if e != nil {
						return e
					}
				}
				return nil
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(zw.Close()).To(Succeed())
			var requests []string
			invalidTree := false
			previous := http.DefaultTransport
			DeferCleanup(func() { http.DefaultTransport = previous })
			http.DefaultTransport = catalogueTransport(func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.URL.String())
				var body string
				switch req.URL.String() {
				case "https://api.github.com/repos/BabisK/idapps/commits/main":
					body = fmt.Sprintf(`{"sha":%q}`, commit)
				case "https://codeload.github.com/BabisK/idapps/zip/" + commit:
					body = archive.String()
					if invalidTree {
						body = "invalid zip"
					}
				default:
					return nil, fmt.Errorf("unexpected request %s", req.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
			})
			GinkgoT().Setenv("DOCKER_CONTEXT", "idapps-test-missing")
			GinkgoT().Setenv("DOCKER_CONFIG", filepath.Join(GinkgoT().TempDir(), "missing"))
			Expect(run([]string{"idapps", "update"})).To(Equal(0))
			paths, err := storage.Resolve()
			Expect(err).NotTo(HaveOccurred())
			store, err := update.NewStore(paths, "1.0.0")
			Expect(err).NotTo(HaveOccurred())
			var apps map[string]catalogue.App
			Expect(store.WithLock(context.Background(), func() error {
				c, actualCommit, err := store.Snapshot()
				if err != nil {
					return err
				}
				Expect(actualCommit).To(Equal(commit))
				apps = c.Apps
				return nil
			})).To(Succeed())
			executable, err := os.Executable()
			Expect(err).NotTo(HaveOccurred())
			for name := range apps {
				target, err := os.Readlink(filepath.Join(paths.Bin, name))
				Expect(err).NotTo(HaveOccurred(), name)
				Expect(target).To(Equal(executable), name)
			}
			Expect(run([]string{"idapps", "update"})).To(Equal(0))
			Expect(os.Remove(filepath.Join(paths.Bin, "psql"))).To(Succeed())
			Expect(run([]string{"idapps", "update"})).To(Equal(0))
			target, err := os.Readlink(filepath.Join(paths.Bin, "psql"))
			Expect(err).NotTo(HaveOccurred())
			Expect(target).To(Equal(executable))
			Expect(requests).To(HaveLen(6))
			invalidTree = true
			Expect(run([]string{"idapps", "update"})).NotTo(Equal(0))
			Expect(store.WithLock(context.Background(), func() error {
				current, actualCommit, err := store.Snapshot()
				if err != nil {
					return err
				}
				Expect(actualCommit).To(Equal(commit))
				Expect(current.Apps).To(Equal(apps))
				return nil
			})).To(Succeed())
			target, err = os.Readlink(filepath.Join(paths.Bin, "psql"))
			Expect(err).NotTo(HaveOccurred())
			Expect(target).To(Equal(executable))
		})
	})

	When("following a retired service's logs", func() {
		var called bool
		BeforeEach(func() {
			p, err := storage.Resolve()
			Expect(err).NotTo(HaveOccurred())
			s, err := update.NewStore(p, "1.0.0")
			Expect(err).NotTo(HaveOccurred())
			err = s.WithLock(context.Background(), func() error {
				id, records, e := s.Resources()
				if e != nil {
					return e
				}
				records["postgres"] = update.ServiceRecord{App: "postgres", Engine: "engine-1", Definition: catalogue.App{Kind: "service", ContainerName: "postgres", Ports: []catalogue.Port{}, Image: catalogue.Image{Repository: "postgres", Tag: "1"}, Network: "bridge", User: catalogue.User{Linux: "image", Darwin: "image", Windows: "image"}}}
				return s.SaveResources(id, records)
			})
			Expect(err).NotTo(HaveOccurred())
			old := followServiceLogs
			DeferCleanup(func() { followServiceLogs = old })
			called = false
			followServiceLogs = func(_ context.Context, store *update.Store, name string, args []string) error {
				called = true
				if name != "postgres" || !reflect.DeepEqual(args, []string{"-f"}) {
					return fmt.Errorf("wrong request: %s %v", name, args)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				return store.WithLock(ctx, func() error { return nil })
			}
		})
		DescribeTable("releases the update lock before following", func(args []string) {
			Expect(run(args)).To(Equal(0))
			Expect(called).To(BeTrue())
		},
			Entry("using direct invocation", []string{"idapps", "postgres", "logs", "-f"}),
			Entry("using the run command", []string{"idapps", "run", "postgres", "logs", "-f"}),
		)
	})
})

var _ = Describe("Cobra app invocation", func() {
	var app *launcher
	var name string
	var arguments []string
	BeforeEach(func() {
		app = &launcher{version: "1.0.0"}
		app.runApp = func(n string, args []string) int {
			name, arguments = n, args
			return 0
		}
	})
	When("an app is selected through the launcher or a link", func() {
		DescribeTable("forwards the app name and arguments unchanged", func(argv []string, goos, want string, args []string) {
			Expect(app.execute(argv, goos)).To(Equal(0))
			Expect(name).To(Equal(want))
			Expect(arguments).To(Equal(args))
		},
			Entry("direct invocation", []string{"idapps", "psql", "--help", "", "--"}, "linux", "psql", []string{"--help", "", "--"}),
			Entry("explicit run", []string{"idapps", "run", "psql", "a b"}, "linux", "psql", []string{"a b"}),
			Entry("symlink", []string{"/bin/psql", "--help"}, "linux", "psql", []string{"--help"}),
			Entry("service symlink without arguments", []string{"/bin/postgres"}, "linux", "postgres", []string{}),
			Entry("Windows executable", []string{"PSQL.EXE", "x"}, "windows", "psql", []string{"x"}),
			Entry("app named help", []string{"idapps", "run", "help", "--help"}, "linux", "help", []string{"--help"}),
			Entry("app named completion", []string{"idapps", "completion", "bash"}, "linux", "completion", []string{"bash"}),
			Entry("app name resembling a flag", []string{"idapps", "run", "--help"}, "linux", "--help", []string{}),
		)
		It("supports every catalogue app through all invocation forms", func() {
			catalogue, err := catalogue.ReadTree("../catalogue", "1.0.0")
			Expect(err).NotTo(HaveOccurred())
			args := []string{"", "space value", "--help", "$literal"}
			for expected := range catalogue.Apps {
				variants := [][]string{append([]string{expected}, args...), append([]string{"idapps", expected}, args...), append([]string{"idapps", "run", expected}, args...)}
				for _, argv := range variants {
					Expect(app.execute(argv, "linux")).To(Equal(0), expected)
					Expect(name).To(Equal(expected))
					Expect(arguments).To(Equal(args))
				}
				argv := append([]string{strings.ToUpper(expected) + ".EXE"}, args...)
				Expect(app.execute(argv, "windows")).To(Equal(0), expected)
				Expect(name).To(Equal(expected))
				Expect(arguments).To(Equal(args))
			}
		})
		It("returns the app's exit status", func() {
			app.runApp = func(string, []string) int { return 42 }
			Expect(app.execute([]string{"idapps", "psql"}, "linux")).To(Equal(42))
		})
	})
	When("a management invocation is invalid", func() {
		DescribeTable("returns usage status 2", func(argv []string) {
			Expect(app.execute(argv, "linux")).To(Equal(2))
		},
			Entry("missing executable", []string{}),
			Entry("run without app", []string{"idapps", "run"}),
			Entry("update with flags", []string{"idapps", "update", "--help"}),
			Entry("version with arguments", []string{"idapps", "version", "extra"}),
			Entry("list with arguments", []string{"idapps", "list", "extra"}),
		)
	})
	When("help is requested without an installed catalogue", func() {
		DescribeTable("prints launcher help successfully", func(argv []string) {
			Expect(app.execute(argv, "linux")).To(Equal(0))
		},
			Entry("no arguments", []string{"idapps"}),
			Entry("help command", []string{"idapps", "help", "extra"}),
			Entry("help flag", []string{"idapps", "--help", "extra"}),
		)
	})
})

var _ = Describe("executing the installed launcher", Ordered, func() {
	var buildDir, binary string
	BeforeAll(func() {
		var err error
		buildDir, err = os.MkdirTemp("", "idapps acceptance with spaces ")
		Expect(err).NotTo(HaveOccurred())
		name := "idapps"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		binary = filepath.Join(buildDir, name)
		build := exec.Command("go", "build", "-o", binary, ".")
		build.Dir = ".."
		output, err := build.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "%s", output)
	})
	AfterAll(func() {
		if buildDir != "" {
			Expect(os.RemoveAll(buildDir)).To(Succeed())
		}
	})
	var paths storage.Paths
	var apps map[string]catalogue.App
	BeforeEach(func() {
		base := GinkgoT().TempDir()
		GinkgoT().Setenv("HOME", base)
		GinkgoT().Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
		GinkgoT().Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
		GinkgoT().Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
		GinkgoT().Setenv("LOCALAPPDATA", filepath.Join(base, "local"))
		GinkgoT().Setenv("APPDATA", filepath.Join(base, "roaming"))
		GinkgoT().Setenv("DOCKER_CONTEXT", "idapps-acceptance-missing")
		GinkgoT().Setenv("DOCKER_CONFIG", filepath.Join(base, "docker"))
		GinkgoT().Setenv("DOCKER_HOST", "")
		var err error
		paths, err = storage.Resolve()
		Expect(err).NotTo(HaveOccurred())
		c, err := catalogue.ReadTree("../catalogue", "0.0.0")
		Expect(err).NotTo(HaveOccurred())
		apps = c.Apps
		files, err := testutil.CatalogueFiles("../catalogue")
		Expect(err).NotTo(HaveOccurred())
		data, err := testutil.CatalogueArchive(files)
		Expect(err).NotTo(HaveOccurred())
		sum := sha256.Sum256(data)
		g := &update.Generation{Commit: strings.Repeat("a", 40), SHA256: hex.EncodeToString(sum[:]), Bytes: data}
		store, err := update.NewStore(paths, "0.0.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(store.WithLock(context.Background(), func() error { return store.Activate(g, binary) })).To(Succeed())
	})
	runProcess := func(executable string, args ...string) (int, string) {
		process := exec.Command(executable, args...)
		output, err := process.CombinedOutput()
		if err == nil {
			return 0, string(output)
		}
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), string(output)
		}
		return -1, fmt.Sprintf("%v: %s", err, output)
	}
	When("a catalogue app is called through the launcher or its symlink", func() {
		It("passes arguments unchanged to Docker through each invocation form", func() {
			listener, host, cleanup, err := listenFakeEngine()
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(cleanup)
			captured := make(chan []string, 3)
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/_ping":
					w.Header().Set("API-Version", "1.47")
					w.Header().Set("OSType", "linux")
				case strings.HasSuffix(r.URL.Path, "/info"):
					_, _ = io.WriteString(w, `{"ID":"test-engine","OSType":"linux"}`)
				case strings.Contains(r.URL.Path, "/images/") && strings.HasSuffix(r.URL.Path, "/json"):
					_, _ = io.WriteString(w, `{}`)
				case strings.HasSuffix(r.URL.Path, "/containers/create"):
					var config struct{ Cmd []string }
					if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					captured <- config.Cmd
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = io.WriteString(w, `{"message":"test captured request"}`)
				default:
					http.Error(w, "unexpected request: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
				}
			})}
			go func() { _ = server.Serve(listener) }()
			DeferCleanup(server.Close)
			GinkgoT().Setenv("DOCKER_CONTEXT", "")
			GinkgoT().Setenv("DOCKER_HOST", host)
			args := []string{"", "space value", "--help", "κόσμος", "$literal;*"}
			linkName := "psql"
			if runtime.GOOS == "windows" {
				linkName += ".exe"
			}
			variants := []struct {
				executable string
				args       []string
			}{
				{filepath.Join(paths.Bin, linkName), args},
				{binary, append([]string{"psql"}, args...)},
				{binary, append([]string{"run", "psql"}, args...)},
			}
			for _, variant := range variants {
				code, output := runProcess(variant.executable, variant.args...)
				Expect(code).To(Equal(125), output)
				Expect(output).To(ContainSubstring("test captured request"))
				Eventually(captured).Should(Receive(Equal(append([]string{"psql"}, args...))))
			}
		})
		DescribeTable("reports invalid service arguments as usage errors", func(args []string) {
			code, output := runProcess(binary, args...)
			Expect(code).To(Equal(2), output)
		},
			Entry("unknown operation", []string{"postgres", "unknown"}),
			Entry("extra start argument", []string{"postgres", "start", "extra"}),
			Entry("invalid log tail", []string{"postgres", "logs", "-t", "-1"}),
		)
		It("reports an unavailable Docker engine as a runtime failure", func() {
			code, output := runProcess(binary, "postgres", "start")
			Expect(code).To(Equal(125), output)
		})
		for _, name := range []string{"aws", "etcd", "etcdctl", "go", "helm", "helmfile", "kubectl", "mariadb-server", "memcached", "mysql", "mysql-server", "mysqldump", "nats", "nats-server", "pg_dump", "pgadmin4", "phpmyadmin", "postgres", "psql", "redis", "redis-cli", "redis-stack"} {
			name := name
			It("selects "+name+" consistently through each invocation form", func() {
				app, exists := apps[name]
				Expect(exists).To(BeTrue(), "app must exist in the catalogue")
				args := []string{"--help", "", "space value", `quote"value`, "κόσμος", "$literal;*"}
				if app.Kind == "service" {
					args = []string{"start"}
				}
				linkName := name
				if runtime.GOOS == "windows" {
					linkName += ".exe"
				}
				link := filepath.Join(paths.Bin, linkName)
				info, err := os.Lstat(link)
				Expect(err).NotTo(HaveOccurred(), name)
				Expect(info.Mode()&os.ModeSymlink).NotTo(BeZero(), name)
				code, output := runProcess(link, args...)
				Expect(code).To(Equal(125), name+": "+output)
				Expect(output).To(ContainSubstring("idapps-acceptance-missing"), name)
				directCode, directOutput := runProcess(binary, append([]string{name}, args...)...)
				Expect(directCode).To(Equal(code), name)
				Expect(directOutput).To(Equal(output), name)
				explicitCode, explicitOutput := runProcess(binary, append([]string{"run", name}, args...)...)
				Expect(explicitCode).To(Equal(code), name)
				Expect(explicitOutput).To(Equal(output), name)
			})
		}
		It("reports an unknown app rather than trying to run Docker", func() {
			code, output := runProcess(binary, "unknown-app")
			Expect(code).To(Equal(2))
			Expect(output).To(ContainSubstring(`unknown app "unknown-app"`))
		})
		It("keeps direct invocation usable when the app link is missing", func() {
			linkName := "psql"
			if runtime.GOOS == "windows" {
				linkName += ".exe"
			}
			Expect(os.Remove(filepath.Join(paths.Bin, linkName))).To(Succeed())
			code, output := runProcess(binary, "psql", "--help")
			Expect(code).To(Equal(125))
			Expect(output).To(ContainSubstring("idapps-acceptance-missing"))
		})
		It("runs the installed launcher without Git, Go or Docker on PATH", func() {
			GinkgoT().Setenv("PATH", GinkgoT().TempDir())
			code, output := runProcess(binary, "list")
			Expect(code).To(Equal(0), output)
			Expect(output).To(ContainSubstring("psql\tcommand"))
			code, output = runProcess(binary, "psql", "--help")
			Expect(code).To(Equal(125), output)
			Expect(output).To(ContainSubstring("idapps-acceptance-missing"))
		})
	})
	When("a shell command has precedence", func() {
		It("leaves earlier executables and functions in place while full paths select idapps", func() {
			if runtime.GOOS == "windows" {
				Skip("POSIX shell precedence")
			}
			shadowDir := filepath.Join(GinkgoT().TempDir(), "shadow")
			Expect(os.MkdirAll(shadowDir, 0700)).To(Succeed())
			shadow := filepath.Join(shadowDir, "psql")
			Expect(os.WriteFile(shadow, []byte("#!/bin/sh\nprintf 'native-shadow\\n'\n"), 0700)).To(Succeed())
			GinkgoT().Setenv("PATH", shadowDir+string(os.PathListSeparator)+paths.Bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			GinkgoT().Setenv("IDAPPS_TEST_LAUNCHER", binary)
			GinkgoT().Setenv("IDAPPS_TEST_LINK", filepath.Join(paths.Bin, "psql"))
			shell := exec.Command("/bin/sh", "-c", `psql; psql() { printf 'function-shadow\n'; }; psql; "$IDAPPS_TEST_LAUNCHER" psql --help; "$IDAPPS_TEST_LINK" --help`)
			output, _ := shell.CombinedOutput()
			Expect(string(output)).To(ContainSubstring("native-shadow\nfunction-shadow\n"))
			Expect(strings.Count(string(output), "idapps-acceptance-missing")).To(Equal(2))
			contents, err := os.ReadFile(shadow)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(contents)).To(ContainSubstring("native-shadow"))
		})
		DescribeTable("preserves Windows executable and function precedence", func(shellName string) {
			if runtime.GOOS != "windows" {
				Skip("Windows PowerShell precedence")
			}
			shellPath, err := exec.LookPath(shellName)
			if err != nil {
				Skip(shellName + " is unavailable")
			}
			shadowDir := filepath.Join(GinkgoT().TempDir(), "shadow")
			Expect(os.MkdirAll(shadowDir, 0700)).To(Succeed())
			source := filepath.Join(shadowDir, "shadow.go")
			Expect(os.WriteFile(source, []byte("package main\nimport \"fmt\"\nfunc main() { fmt.Println(\"native-shadow\") }\n"), 0600)).To(Succeed())
			build := exec.Command("go", "build", "-o", filepath.Join(shadowDir, "psql.exe"), source)
			output, err := build.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), "%s", output)
			GinkgoT().Setenv("PATH", shadowDir+string(os.PathListSeparator)+paths.Bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			GinkgoT().Setenv("IDAPPS_TEST_LAUNCHER", binary)
			GinkgoT().Setenv("IDAPPS_TEST_LINK", filepath.Join(paths.Bin, "psql.exe"))
			shell := exec.Command(shellPath, "-NoProfile", "-NonInteractive", "-Command", `psql; Set-Alias -Name psql -Value Write-Output; psql 'alias-shadow'; Remove-Item Alias:psql; function psql { 'function-shadow' }; psql; & $env:IDAPPS_TEST_LAUNCHER psql --help; & $env:IDAPPS_TEST_LINK --help`)
			output, _ = shell.CombinedOutput()
			Expect(string(output)).To(ContainSubstring("native-shadow"))
			Expect(string(output)).To(ContainSubstring("alias-shadow"))
			Expect(string(output)).To(ContainSubstring("function-shadow"))
			Expect(strings.Count(string(output), "idapps-acceptance-missing")).To(Equal(2), "%s", output)
		},
			Entry("in Windows PowerShell 5.1", "powershell.exe"),
			Entry("in PowerShell 7", "pwsh.exe"),
		)
	})
})
