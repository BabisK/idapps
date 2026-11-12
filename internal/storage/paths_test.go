package storage

import (
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Per-user storage paths", func() {
	When("the launcher reads environment configuration on Linux", func() {
		BeforeEach(func() {
			if runtime.GOOS != "linux" {
				Skip("Linux XDG configuration")
			}
			base := GinkgoT().TempDir()
			GinkgoT().Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
			GinkgoT().Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
			GinkgoT().Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
		})
		It("uses the configured XDG directories", func() {
			paths, err := Resolve()
			Expect(err).NotTo(HaveOccurred())
			Expect(paths.Cache).To(Equal(filepath.Join(os.Getenv("XDG_CACHE_HOME"), "idapps", "catalogues")))
			Expect(paths.Config).To(Equal(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "idapps")))
			Expect(paths.Bin).To(Equal(filepath.Join(os.Getenv("XDG_DATA_HOME"), "idapps", "bin")))
		})
		It("rejects relative configured paths", func() {
			GinkgoT().Setenv("XDG_CONFIG_HOME", "relative")
			_, err := Resolve()
			Expect(err).To(MatchError(ContainSubstring("XDG_CONFIG_HOME")))
		})
		It("uses the home directory default when an override is empty", func() {
			GinkgoT().Setenv("XDG_CACHE_HOME", "")
			home, err := os.UserHomeDir()
			Expect(err).NotTo(HaveOccurred())
			paths, err := Resolve()
			Expect(err).NotTo(HaveOccurred())
			Expect(paths.Cache).To(Equal(filepath.Join(home, ".cache", "idapps", "catalogues")))
		})
	})
	When("running on Linux", func() {
		var env map[string]string
		BeforeEach(func() {
			if runtime.GOOS == "windows" {
				Skip("POSIX path semantics")
			}
			env = map[string]string{"XDG_CACHE_HOME": "/cache", "XDG_CONFIG_HOME": "/config", "XDG_DATA_HOME": "/data"}
		})
		It("uses the configured XDG directories", func() {
			paths, err := resolve("linux", func(k string) string { return env[k] }, func() (string, error) { return "/home/user", nil })
			Expect(err).NotTo(HaveOccurred())
			Expect(paths).To(Equal(Paths{Cache: "/cache/idapps/catalogues", Config: "/config/idapps", Bin: "/data/idapps/bin"}))
		})
		When("the XDG data directory is relative", func() {
			It("reports the invalid environment variable", func() {
				env["XDG_DATA_HOME"] = "relative"
				_, err := resolve("linux", func(k string) string { return env[k] }, func() (string, error) { return "/home/user", nil })
				Expect(err).To(MatchError(ContainSubstring("XDG_DATA_HOME")))
			})
		})
	})
	When("running on macOS", func() {
		It("uses the user's Library for installed app links", func() {
			if runtime.GOOS == "windows" {
				Skip("POSIX path semantics")
			}
			paths, err := resolve("darwin", func(string) string { return "" }, func() (string, error) { return "/home/user", nil })
			Expect(err).NotTo(HaveOccurred())
			Expect(paths.Bin).To(Equal(filepath.Join("/home/user/Library/Application Support/idapps/bin")))
		})
	})
	When("running on Windows", func() {
		It("reads local and roaming directories from the environment", func() {
			if runtime.GOOS != "windows" {
				Skip("Windows path semantics")
			}
			base := GinkgoT().TempDir()
			local, roaming := filepath.Join(base, "local"), filepath.Join(base, "roaming")
			GinkgoT().Setenv("LOCALAPPDATA", local)
			GinkgoT().Setenv("APPDATA", roaming)
			paths, err := Resolve()
			Expect(err).NotTo(HaveOccurred())
			Expect(paths).To(Equal(Paths{Cache: filepath.Join(local, "idapps", "cache", "catalogues"), Config: filepath.Join(roaming, "idapps"), Bin: filepath.Join(local, "idapps", "bin")}))
		})
		It("uses local and roaming application data directories", func() {
			if runtime.GOOS != "windows" {
				Skip("Windows path semantics")
			}
			env := map[string]string{"LOCALAPPDATA": `C:\Local`, "APPDATA": `C:\Roaming`}
			paths, err := resolve("windows", func(k string) string { return env[k] }, func() (string, error) { return `C:\Users\test`, nil })
			Expect(err).NotTo(HaveOccurred())
			Expect(paths).To(Equal(Paths{Cache: filepath.Join(`C:\Local`, "idapps", "cache", "catalogues"), Config: filepath.Join(`C:\Roaming`, "idapps"), Bin: filepath.Join(`C:\Local`, "idapps", "bin")}))
		})
	})
})
