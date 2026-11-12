package engine

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Docker endpoint selection", func() {
	DescribeTable("accepts local Engine transports", func(host string) {
		_, err := validateEndpoint(Endpoint{Host: host})
		Expect(err).NotTo(HaveOccurred())
	},
		Entry("the system Unix socket", "unix:///var/run/docker.sock"),
		Entry("a user Unix socket", "unix:///home/user/.docker/run/docker.sock"),
		Entry("the local Windows named pipe", "npipe:////./pipe/docker_engine"),
	)
	DescribeTable("rejects unsupported or remote transports", func(host string) {
		_, err := validateEndpoint(Endpoint{Host: host})
		Expect(err).To(HaveOccurred())
	},
		Entry("TCP", "tcp://127.0.0.1:2375"),
		Entry("SSH", "ssh://host"),
		Entry("HTTP", "http://host"),
		Entry("a remote Unix socket", "unix://remote/tmp/docker.sock"),
		Entry("a remote Windows named pipe", "npipe:////remote/pipe/docker_engine"),
	)

	When("a Docker context is selected", func() {
		BeforeEach(func() {
			dir := GinkgoT().TempDir()
			GinkgoT().Setenv("DOCKER_CONFIG", dir)
			GinkgoT().Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
			meta := filepath.Join(dir, "contexts", "meta", "remote")
			Expect(os.MkdirAll(meta, 0700)).To(Succeed())
			data := `{"Name":"remote","Endpoints":{"docker":{"Host":"ssh://user:do-not-expose@example.invalid:22"}}}`
			Expect(os.WriteFile(filepath.Join(meta, "meta.json"), []byte(data), 0600)).To(Succeed())
		})
		It("rejects a remote context without exposing its credentials", func() {
			GinkgoT().Setenv("DOCKER_CONTEXT", "remote")
			_, err := SelectEndpoint()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring("do-not-expose"))
		})
		It("reports a missing context instead of falling back to DOCKER_HOST", func() {
			GinkgoT().Setenv("DOCKER_CONTEXT", "missing")
			_, err := SelectEndpoint()
			Expect(err).To(MatchError(ContainSubstring("missing")))
		})
		It("uses a selected local context before DOCKER_HOST", func() {
			meta := filepath.Join(os.Getenv("DOCKER_CONFIG"), "contexts", "meta", "local")
			Expect(os.MkdirAll(meta, 0700)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(meta, "meta.json"), []byte(`{"Name":"local","Endpoints":{"docker":{"Host":"unix:///tmp/selected.sock"}}}`), 0600)).To(Succeed())
			GinkgoT().Setenv("DOCKER_CONTEXT", "local")
			ep, err := SelectEndpoint()
			Expect(err).NotTo(HaveOccurred())
			Expect(ep).To(Equal(Endpoint{Context: "local", Host: "unix:///tmp/selected.sock"}))
		})
	})
	When("DOCKER_HOST contains a remote endpoint", func() {
		It("rejects the endpoint without exposing credentials or tokens", func() {
			GinkgoT().Setenv("DOCKER_CONTEXT", "")
			GinkgoT().Setenv("DOCKER_HOST", "tcp://user:do-not-expose@example.invalid:2375?token=secret-value")
			_, err := SelectEndpoint()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring("do-not-expose"))
			Expect(err.Error()).NotTo(ContainSubstring("secret-value"))
		})
	})
})
