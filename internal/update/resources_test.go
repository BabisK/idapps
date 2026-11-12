package update

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/BabisK/idapps/internal/catalogue"
)

var _ = Describe("saved service definitions", func() {
	var store *Store
	var definition catalogue.App
	BeforeEach(func() {
		var err error
		store, _ = testStore(GinkgoT())
		data, err := os.ReadFile("../../catalogue.json")
		Expect(err).NotTo(HaveOccurred())
		c, err := catalogue.Parse(data, "0.0.0")
		Expect(err).NotTo(HaveOccurred())
		definition = c.Apps["postgres"]
	})
	When("the catalogue cache is unavailable", func() {
		It("still accepts a valid retired service snapshot", func() {
			Expect(store.WithLock(context.Background(), func() error {
				id, records, err := store.Resources()
				if err != nil {
					return err
				}
				records["postgres"] = ServiceRecord{App: "postgres", Engine: "engine", Definition: definition}
				if err := store.SaveResources(id, records); err != nil {
					return err
				}
				_, _, err = store.Resources()
				return err
			})).To(Succeed())
		})
		DescribeTable("rejects a corrupted snapshot", func(corrupt func(*catalogue.App)) {
			corrupt(&definition)
			err := store.WithLock(context.Background(), func() error {
				id, records, err := store.Resources()
				if err != nil {
					return err
				}
				records["postgres"] = ServiceRecord{App: "postgres", Engine: "engine", Definition: definition}
				if err := store.SaveResources(id, records); err != nil {
					return err
				}
				_, _, err = store.Resources()
				return err
			})
			Expect(err).To(MatchError(ContainSubstring("invalid service record")))
		},
			Entry("missing image", func(a *catalogue.App) { a.Image.Repository = "" }),
			Entry("invalid network", func(a *catalogue.App) { a.Network = "remote" }),
			Entry("invalid platform user", func(a *catalogue.App) { a.User.Linux = "host" }),
			Entry("missing ports", func(a *catalogue.App) { a.Ports = nil }),
			Entry("invalid volume target", func(a *catalogue.App) { a.Mounts[0].Target = "relative" }),
		)
	})
})

var _ = Describe("managed service identity", func() {
	var original catalogue.App
	BeforeEach(func() {
		original = catalogue.App{Kind: "service", ContainerName: "db", Mounts: []catalogue.Mount{{Type: "volume", Name: "data"}}}
	})

	When("only the image changes", func() {
		It("keeps the existing resource identity", func() {
			updated := original
			updated.Image.Repository = "new-image"
			Expect(sameIdentity(original, updated)).To(BeTrue())
		})
	})

	When("a resource name changes", func() {
		DescribeTable("recognizes a different resource identity", func(change func(*catalogue.App)) {
			updated := original
			change(&updated)
			Expect(sameIdentity(original, updated)).To(BeFalse())
		},
			Entry("the container name changes", func(app *catalogue.App) { app.ContainerName = "other" }),
			Entry("the volume name changes", func(app *catalogue.App) {
				app.Mounts = []catalogue.Mount{{Type: "volume", Name: "other"}}
			}),
		)
	})
})
