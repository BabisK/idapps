package catalogue

import (
	"bytes"
	"encoding/json"
	"github.com/google/jsonschema-go/jsonschema"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Catalogue", func() {
	It("loads every app from individual descriptors", func() {
		c, err := ReadTree("../../catalogue", "1.0.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Apps).To(HaveLen(22))
	})
	When("checking launcher compatibility", func() {
		DescribeTable("compares semantic versions", func(minimum, launcher string, compatible bool) {
			files := map[string][]byte{}
			Expect(filepath.WalkDir("../../catalogue", func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || d.Name() == "schema_embed.go" {
					return nil
				}
				rel, err := filepath.Rel("../../catalogue", p)
				if err != nil {
					return err
				}
				files[filepath.ToSlash(rel)], err = os.ReadFile(p)
				return err
			})).To(Succeed())
			files["metadata.json"] = []byte(`{"schemaVersion":1,"minimumLauncherVersion":"` + minimum + `"}`)
			_, err := ParseTree(files, launcher)
			if compatible {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
			}
		},
			Entry("newer release", "1.2.0", "1.10.0", true),
			Entry("pre-release below final", "1.2.0", "1.2.0-rc.1", false),
			Entry("ordered pre-release", "1.2.0-rc.2", "1.2.0-rc.10", true),
			Entry("build metadata", "1.2.0+other", "1.2.0+commit.dirty", true),
			Entry("invalid version", "1.2", "1.2.0", false),
			Entry("invalid launcher", "1.0.0", "development", false),
		)
	})
})

var _ = Describe("assembling catalogue descriptors", func() {
	var files map[string][]byte
	BeforeEach(func() {
		files = map[string][]byte{}
		err := filepath.WalkDir("../../catalogue", func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel("../../catalogue", p)
			if err != nil {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if filepath.ToSlash(rel) != "schema_embed.go" {
				files[filepath.ToSlash(rel)] = b
			}
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	})
	It("loads the same apps from files and a filesystem tree", func() {
		got, err := ParseTree(files, "1.0.0")
		Expect(err).NotTo(HaveOccurred())
		want, err := ReadTree("../../catalogue", "1.0.0")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(want))
	})
	DescribeTable("rejects invalid individual inputs", func(name, before, after string) {
		original := string(files[name])
		changed := strings.Replace(original, before, after, 1)
		Expect(changed).NotTo(Equal(original))
		files[name] = []byte(changed)
		_, err := ParseTree(files, "1.0.0")
		Expect(err).To(HaveOccurred())
	},
		Entry("unknown metadata", "metadata.json", `"schemaVersion": 1`, `"unknown": true, "schemaVersion": 1`),
		Entry("string schema version", "metadata.json", `"schemaVersion": 1`, `"schemaVersion": "1"`),
		Entry("unsupported kind", "etcd/app.json", `"kind": "service"`, `"kind": "other"`),
		Entry("host network with port bindings", "etcd/app.json", `"network": "bridge"`, `"network": "host"`),
		Entry("port above maximum", "etcd/app.json", `"hostPort": 2379`, `"hostPort": 65536`),
		Entry("fractional port", "etcd/app.json", `"hostPort": 2379`, `"hostPort": 2379.5`),
		Entry("barely fractional port", "etcd/app.json", `"hostPort": 2379`, `"hostPort": 65535.0000000000000001`),
		Entry("NUL argument", "etcd/app.json", `"--listen-client-urls"`, `"bad\u0000argument"`),
		Entry("trailing document", "etcd/app.json", `"kind": "service"`, `"kind": "service"`+"} {} {"),
	)
	It("rejects reserved app names", func() {
		files["help/app.json"] = files["aws/app.json"]
		delete(files, "aws/app.json")
		_, err := ParseTree(files, "1.0.0")
		Expect(err).To(MatchError(ContainSubstring("app name")))
	})
	It("rejects a fractional schema version that rounds to one", func() {
		files["metadata.json"] = []byte(`{"schemaVersion":1.0000000000000001,"minimumLauncherVersion":"0.0.0"}`)
		_, err := ParseTree(files, "1.0.0")
		Expect(err).To(HaveOccurred())
	})
	It("rejects malformed and duplicate keys before assembly", func() {
		files["psql/app.json"] = []byte(`{"kind":"command","kind":"service"}`)
		_, err := ParseTree(files, "1.0.0")
		Expect(err).To(MatchError(ContainSubstring("duplicate")))
	})
	It("rejects missing metadata and unexpected paths", func() {
		delete(files, "metadata.json")
		_, err := ParseTree(files, "1.0.0")
		Expect(err).To(HaveOccurred())
		files["metadata.json"], err = os.ReadFile("../../catalogue/metadata.json")
		Expect(err).NotTo(HaveOccurred())
		files["../evil/app.json"] = []byte(`{}`)
		_, err = ParseTree(files, "1.0.0")
		Expect(err).To(HaveOccurred())
	})
	It("rejects duplicate container names across descriptors", func() {
		files["redis-cli/app.json"] = files["redis/app.json"]
		_, err := ParseTree(files, "1.0.0")
		Expect(err).To(MatchError(ContainSubstring("share container name")))
	})
	It("rejects duplicate volume names across descriptors", func() {
		var app map[string]any
		Expect(json.Unmarshal(files["redis/app.json"], &app)).To(Succeed())
		app["containerName"] = "another-redis"
		var err error
		files["redis-cli/app.json"], err = json.Marshal(app)
		Expect(err).NotTo(HaveOccurred())
		_, err = ParseTree(files, "1.0.0")
		Expect(err).To(MatchError(ContainSubstring("share volume")))
	})
})

var _ = Describe("reading a catalogue tree", func() {
	It("rejects a symlinked app descriptor", func() {
		root := GinkgoT().TempDir()
		Expect(os.Mkdir(filepath.Join(root, "example"), 0755)).To(Succeed())
		err := os.Symlink(filepath.Join(root, "target.json"), filepath.Join(root, "example", "app.json"))
		if err != nil {
			Skip("symlink creation is unavailable: " + err.Error())
		}
		_, err = ReadTree(root, "1.0.0")
		Expect(err).To(MatchError(ContainSubstring("non-regular")))
	})
})

var _ = Describe("the published descriptor schema", func() {
	It("validates every descriptor and shares definitions with runtime validation", func() {
		published, err := os.ReadFile("../../catalogue/catalogue.schema.json")
		Expect(err).NotTo(HaveOccurred())
		var doc jsonschema.Schema
		Expect(json.Unmarshal(bytes.ReplaceAll(published, []byte(`\\u0000`), []byte(`\\x00`)), &doc)).To(Succeed())
		schema, err := doc.Resolve(nil)
		Expect(err).NotTo(HaveOccurred())
		paths, err := filepath.Glob("../../catalogue/*/app.json")
		Expect(err).NotTo(HaveOccurred())
		Expect(paths).NotTo(BeEmpty())
		for _, p := range paths {
			data, err := os.ReadFile(p)
			Expect(err).NotTo(HaveOccurred())
			var value any
			Expect(json.Unmarshal(data, &value)).To(Succeed())
			Expect(schema.Validate(value)).To(Succeed(), p)
		}
	})
})
