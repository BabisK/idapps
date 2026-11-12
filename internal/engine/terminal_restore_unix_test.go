//go:build linux || darwin

package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/creack/pty"
	"github.com/docker/docker/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/term"

	"github.com/BabisK/idapps/internal/catalogue"
)

var _ = Describe("interactive terminals", func() {
	DescribeTable("restores terminal mode and removes the owned container", func(mode string) {
		t := GinkgoT()
		master, slave, err := pty.Open()
		Expect(err).NotTo(HaveOccurred(), "open pseudo-terminal: %v", err)

		defer master.Close()
		defer slave.Close()
		{
			err = pty.Setsize(slave, &pty.Winsize{Rows: 25, Cols: 80})
			Expect(err).NotTo(HaveOccurred())
		}

		before, err := term.GetState(int(slave.Fd()))
		Expect(err).NotTo(HaveOccurred())

		oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
		os.Stdin, os.Stdout, os.Stderr = slave, slave, slave
		defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()
		oldFactory := openEngine
		defer func() { openEngine = oldFactory }()
		started := make(chan struct{})
		created := make(chan struct {
			Tty        bool
			HostConfig struct{ ConsoleSize [2]uint }
		}, 1)
		removed := make(chan string, 1)
		dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
			local, remote := net.Pipe()
			go func() {
				defer remote.Close()
				r, e := http.ReadRequest(bufio.NewReader(remote))
				if e != nil {
					return
				}
				defer r.Body.Close()
				body, _ := io.ReadAll(r.Body)
				p := r.URL.Path
				if strings.HasSuffix(p, "/attach") {
					_, _ = fmt.Fprint(remote, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: tcp\r\nContent-Type: application/vnd.docker.raw-stream\r\n\r\n")
					if mode == "success" {
						<-started
						_, _ = io.WriteString(remote, "done")
						return
					}
					_, _ = io.Copy(io.Discard, remote)
					return
				}
				if strings.HasSuffix(p, "/wait") {
					if mode == "success" {
						body := `{"StatusCode":7}`
						_, _ = fmt.Fprintf(remote, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n", len(body))
						<-started
						_, _ = io.WriteString(remote, body)
						return
					}
					_, _ = fmt.Fprint(remote, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n")
					_, _ = io.Copy(io.Discard, remote)
					return
				}
				status, data := "200 OK", "{}"
				switch {
				case strings.HasSuffix(p, "/containers/create"):
					var cfg struct {
						Tty        bool
						HostConfig struct{ ConsoleSize [2]uint }
					}
					_ = json.Unmarshal(body, &cfg)
					created <- cfg
					data = `{"Id":"owned-tty"}`
				case strings.HasSuffix(p, "/start"):
					defer close(started)
					if mode == "start-failure" {
						status = "500 Internal Server Error"
						data = `{"message":"injected start failure"}`
					} else {
						status = "204 No Content"
						data = ""
					}
				case strings.HasSuffix(p, "/kill"):
					status = "204 No Content"
					data = ""
				case r.Method == "DELETE":
					removed <- p
					status = "204 No Content"
					data = ""
				}
				_, _ = fmt.Fprintf(remote, "HTTP/1.1 %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", status, len(data), data)
			}()
			return local, nil
		}
		openEngine = func(context.Context) (*client.Client, error) {
			return client.NewClientWithOpts(client.WithHost("http://docker"), client.WithVersion("1.47"), client.WithHTTPClient(&http.Client{Transport: &http.Transport{DialContext: dial}}))
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
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
			t.Fatal("start request not received")
		}
		if mode == "cancellation" {
			raw, err := term.GetState(int(slave.Fd()))
			Expect(err).NotTo(HaveOccurred())
			Expect(reflect.DeepEqual(before, raw)).To(BeFalse(), "terminal did not enter raw mode")

			cancel()
		}
		select {
		case got := <-done:
			if mode == "cancellation" {
				Expect(got.code != 130 || !errors.Is(got.err, context.Canceled)).To(BeFalse(), "cancel: %d %v", got.code, got.err)

			} else if mode == "success" {
				Expect(got.code != 7 || got.err != nil).To(BeFalse(), "success: %d %v", got.code, got.err)

			} else {
				Expect(got.code != 125 || got.err == nil || !strings.Contains(got.err.Error(), "injected start failure")).To(BeFalse(), "start failure: %d %v", got.code, got.err)
			}

		case <-time.After(5 * time.Second):
			t.Fatal("launcher did not return")
		}
		after, err := term.GetState(int(slave.Fd()))
		Expect(err).NotTo(HaveOccurred())
		Expect(!reflect.DeepEqual(before, after)).To(BeFalse(), "terminal mode was not restored")

		cfg := <-created
		Expect(!cfg.Tty || cfg.HostConfig.ConsoleSize != [2]uint{25, 80}).To(BeFalse(), "incorrect TTY create settings: %+v", cfg)

		select {
		case path := <-removed:
			Expect(!strings.HasSuffix(path, "/containers/owned-tty")).To(BeFalse(), "removed unexpected container %s", path)

		case <-time.After(2 * time.Second):
			t.Fatal("owned container was not removed")
		}
	},
		Entry("after the container fails to start", "start-failure"),
		Entry("after cancellation", "cancellation"),
		Entry("after successful execution", "success"),
	)
	Context("when deciding whether to allocate a terminal", func() {
		var master, slave, reader, writer *os.File
		BeforeEach(func() {
			var err error
			master, slave, err = pty.Open()
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { master.Close(); slave.Close() })
			reader, writer, err = os.Pipe()
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { reader.Close(); writer.Close() })
		})
		DescribeTable("requires all three streams to be terminals", func(which string, want bool) {
			in, out, errout := slave, slave, slave
			switch which {
			case "stdin":
				in = reader
			case "stdout":
				out = writer
			case "stderr":
				errout = writer
			case "all":
				in, out, errout = reader, writer, writer
			}
			Expect(terminalStreams(in, out, errout)).To(Equal(want))
		},
			Entry("all are terminals", "none", true),
			Entry("stdin is a pipe", "stdin", false),
			Entry("stdout is a pipe", "stdout", false),
			Entry("stderr is a pipe", "stderr", false),
			Entry("all are pipes", "all", false),
		)
	})
})
