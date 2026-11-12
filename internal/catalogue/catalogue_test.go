package catalogue

import (
	"encoding/json"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Catalogue", func() {
	var document []byte
	BeforeEach(func() {
		var err error
		document, err = os.ReadFile("../../catalogue.json")
		Expect(err).NotTo(HaveOccurred())
	})

	When("the production catalogue is loaded", func() {
		It("accepts all 22 apps", func() {
			c, err := Parse(document, "1.0.0")
			Expect(err).NotTo(HaveOccurred())
			Expect(c.Apps).To(HaveLen(22))
		})
		It("accepts a development build before the first release", func() {
			_, err := Parse(document, "0.0.0+abcdef.dirty")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("the catalogue document is invalid", func() {
		DescribeTable("rejects the document", func(change func(string) string) {
			changed := change(string(document))
			Expect(changed).NotTo(Equal(string(document)), "the scenario must change the catalogue")
			_, err := Parse([]byte(changed), "1.0.0")
			Expect(err).To(HaveOccurred())
		},
			Entry("when a key is duplicated", func(s string) string {
				return strings.Replace(s, `"schemaVersion": 1,`, `"schemaVersion": 1, "schemaVersion": 1,`, 1)
			}),
			Entry("when a field is unknown", func(s string) string {
				return strings.Replace(s, `"schemaVersion": 1,`, `"unknown": true, "schemaVersion": 1,`, 1)
			}),
			Entry("when an app uses a reserved name", func(s string) string { return strings.Replace(s, `"aws": {`, `"help": {`, 1) }),
			Entry("when the launcher version is too old", func(s string) string {
				return strings.Replace(s, `"minimumLauncherVersion": "0.0.0"`, `"minimumLauncherVersion": "999.0.0"`, 1)
			}),
			Entry("when another JSON value follows the catalogue", func(s string) string { return s + " {}" }),
			Entry("when an argument contains a NUL byte", func(s string) string {
				return strings.Replace(s, `"network": "host"`, `"args": ["bad\u0000argument"], "network": "host"`, 1)
			}),
			Entry("when a required kind is invalid", func(s string) string {
				return strings.Replace(s, `"kind": "command"`, `"kind": "other"`, 1)
			}),
			Entry("when a service on host networking exposes ports", func(s string) string {
				return strings.Replace(s, `"network": "bridge"`, `"network": "host"`, 1)
			}),
			Entry("when a port exceeds its maximum", func(s string) string {
				return strings.Replace(s, `"hostPort": 2379`, `"hostPort": 65536`, 1)
			}),
			Entry("when a port is just above its maximum", func(s string) string {
				return strings.Replace(s, `"hostPort": 2379`, `"hostPort": 65535.0000000000000001`, 1)
			}),
			Entry("when a port is fractional", func(s string) string {
				return strings.Replace(s, `"hostPort": 2379`, `"hostPort": 2379.5`, 1)
			}),
			Entry("when a schema version is a string", func(s string) string {
				return strings.Replace(s, `"schemaVersion": 1`, `"schemaVersion": "1"`, 1)
			}),
		)
	})
	When("a port uses a valid boundary value", func() {
		DescribeTable("accepts the catalogue", func(port string) {
			changed := strings.Replace(string(document), `"hostPort": 2379`, `"hostPort": `+port, 1)
			Expect(changed).NotTo(Equal(string(document)))
			_, err := Parse([]byte(changed), "1.0.0")
			Expect(err).NotTo(HaveOccurred())
		},
			Entry("minimum port", "1"),
			Entry("maximum port", "65535"),
		)
	})
	When("checking launcher compatibility", func() {
		DescribeTable("compares semantic versions", func(minimum, launcher string, compatible bool) {
			changed := strings.Replace(string(document), `"minimumLauncherVersion": "0.0.0"`, `"minimumLauncherVersion": "`+minimum+`"`, 1)
			if minimum != "0.0.0" {
				Expect(changed).NotTo(Equal(string(document)))
			}
			_, err := Parse([]byte(changed), launcher)
			if compatible {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
			}
		},
			Entry("newer release", "1.2.0", "1.10.0", true),
			Entry("pre-release below the final release", "1.2.0", "1.2.0-rc.1", false),
			Entry("ordered pre-release identifiers", "1.2.0-rc.2", "1.2.0-rc.10", true),
			Entry("release after a pre-release", "1.2.0-rc.1", "1.2.0", true),
			Entry("build metadata has no precedence", "1.2.0+other", "1.2.0+commit.dirty", true),
			Entry("large numeric components retain exact ordering", "12345678901234567890.0.0", "12345678901234567891.0.0", true),
			Entry("numeric pre-release identifiers sort before text", "1.2.0-rc.alpha", "1.2.0-rc.123", false),
			Entry("leading zero in pre-release", "1.2.0-rc.01", "1.2.0", false),
			Entry("missing patch", "1.2", "1.2.0", false),
			Entry("invalid launcher version", "1.0.0", "development", false),
		)
	})

	When("the schema is distributed", func() {
		It("matches the schema embedded in the launcher", func() {
			published, err := os.ReadFile("../../catalogue.schema.json")
			Expect(err).NotTo(HaveOccurred())
			Expect(schemaBytes).To(Equal(published))
		})
	})

	When("the MySQL client image is updated", func() {
		It("leaves the server and mysqldump images unchanged", func() {
			var doc map[string]any
			Expect(json.Unmarshal(document, &doc)).To(Succeed())
			original, err := Parse(document, "1.0.0")
			Expect(err).NotTo(HaveOccurred())
			apps := doc["apps"].(map[string]any)
			apps["mysql"].(map[string]any)["image"].(map[string]any)["tag"] = "0.0.0-fixture"
			updatedDocument, err := json.Marshal(doc)
			Expect(err).NotTo(HaveOccurred())
			updated, err := Parse(updatedDocument, "1.0.0")
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Apps["mysql"].Image.Tag).To(Equal("0.0.0-fixture"))
			for _, name := range []string{"mysql-server", "mysqldump"} {
				Expect(updated.Apps[name].Image).To(Equal(original.Apps[name].Image), name)
			}
		})
	})
})
