package compat

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("catalogue compatibility with releases", func() {
	var repo, proposed string
	var document map[string]any
	gitRun := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		output, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "%s", output)
	}
	writeCatalogue := func() {
		data, err := json.Marshal(document)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(proposed, data, 0600)).To(Succeed())
	}
	BeforeEach(func() {
		repo = GinkgoT().TempDir()
		proposed = filepath.Join(GinkgoT().TempDir(), "catalogue.json")
		data, err := os.ReadFile("../../catalogue.json")
		Expect(err).NotTo(HaveOccurred())
		Expect(json.Unmarshal(data, &document)).To(Succeed())
		writeCatalogue()
		gitRun("init", "-q")
		Expect(os.MkdirAll(filepath.Join(repo, "internal", "catalogue"), 0700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.test/old\n\ngo 1.26.8\n"), 0600)).To(Succeed())
		parser := `package catalogue
import ("errors"; "strings")
func Parse(data []byte, version string) (any, error) {
 if strings.Contains(string(data), "new-incompatible") { return nil, errors.New("unsupported app") }; return nil, nil
}`
		Expect(os.WriteFile(filepath.Join(repo, "internal", "catalogue", "catalogue.go"), []byte(parser), 0600)).To(Succeed())
		gitRun("add", ".")
		gitRun("commit", "-qm", "fixture")
	})
	When("no release exists", func() {
		It("checks the catalogue with the current parser", func() { Expect(Check(repo, proposed)).To(Succeed()) })
		It("rejects a catalogue the current parser cannot read", func() {
			document["schemaVersion"] = 2
			writeCatalogue()
			Expect(Check(repo, proposed)).To(MatchError(ContainSubstring("current parser")))
		})
	})
	When("an older release is supported", func() {
		BeforeEach(func() { gitRun("tag", "v1.0.0") })
		It("accepts a catalogue understood by that release", func() { Expect(Check(repo, proposed)).To(Succeed()) })
		It("rejects a catalogue the release cannot parse", func() {
			apps := document["apps"].(map[string]any)
			apps["new-incompatible"] = apps["aws"]
			writeCatalogue()
			err := Check(repo, proposed)
			Expect(err).To(MatchError(ContainSubstring("catalogue incompatible with v1.0.0")))
			Expect(err.Error()).To(ContainSubstring("unsupported app"))
		})
		It("ignores releases older than the declared minimum", func() {
			apps := document["apps"].(map[string]any)
			apps["new-incompatible"] = apps["aws"]
			document["minimumLauncherVersion"] = "2.0.0"
			writeCatalogue()
			Expect(Check(repo, proposed)).To(Succeed())
		})
	})
	When("a Git tag is not a complete semantic version", func() {
		It("does not treat it as a release", func() { Expect(validTag("v1.2")).To(BeFalse()); Expect(validTag("v1.2.3-rc.1")).To(BeTrue()) })
	})
})
