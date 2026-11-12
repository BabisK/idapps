//go:build darwin

package update

import (
	"path/filepath"

	"github.com/BabisK/idapps/internal/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("macOS host identity", func() {
	It("provides a stable identity for opening an installation", func() {
		first, err := machineID()
		Expect(err).NotTo(HaveOccurred())
		Expect(first).NotTo(BeEmpty())
		second, err := machineID()
		Expect(err).NotTo(HaveOccurred())
		Expect(second).To(Equal(first))
		base := GinkgoT().TempDir()
		paths := storage.Paths{Config: filepath.Join(base, "config", "idapps")}
		_, err = NewStore(paths, "0.0.0")
		Expect(err).NotTo(HaveOccurred())
	})
})
