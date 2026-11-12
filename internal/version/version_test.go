package version

import (
	"runtime/debug"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Build version", func() {
	When("Go embeds repository metadata", func() {
		DescribeTable("reports release and development builds", func(moduleVersion, revision string, dirty bool, expected string) {
			modified := "false"
			if dirty {
				modified = "true"
			}
			info := &debug.BuildInfo{Main: debug.Module{Version: moduleVersion}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: revision}, {Key: "vcs.modified", Value: modified}}}
			Expect(FromBuildInfo(info)).To(Equal(expected))
		},
			Entry("tagged release", "v1.2.3", "abc123", false, "1.2.3"),
			Entry("commit after a release", "v1.2.4-0.20261110192659-abcdef123456", "abcdef1234567890", false, "1.2.3+abcdef1234567890"),
			Entry("dirty build at a tag", "v1.2.3+dirty", "abcdef1234567890", true, "1.2.3+abcdef1234567890.dirty"),
			Entry("dirty commit after a release", "v1.2.4-0.20261110192659-abcdef123456+dirty", "abcdef1234567890", true, "1.2.3+abcdef1234567890.dirty"),
			Entry("a pseudo-version without a VCS revision", "v1.2.4-0.20261110192659-abcdef123456", "", false, "1.2.3+abcdef123456"),
			Entry("dirty incompatible module version", "v2.0.1-0.20261110192659-abcdef123456+incompatible.dirty", "abcdef1234567890", true, "2.0.0+abcdef1234567890.dirty"),
			Entry("pre-release tag", "v1.2.3-rc.1", "abcdef1234567890", false, "1.2.3-rc.1"),
			Entry("commit after a pre-release", "v1.2.3-rc.1.0.20261110192659-abcdef123456", "abcdef1234567890", false, "1.2.3-rc.1+abcdef1234567890"),
			Entry("before the first release", "v0.0.0-20261110192659-abcdef123456", "abcdef1234567890", false, "0.0.0+abcdef1234567890"),
			Entry("before the first release with uncommitted changes", "v0.0.0-20261110192659-abcdef123456+dirty", "abcdef1234567890", true, "0.0.0+abcdef1234567890.dirty"),
			Entry("without metadata", "(devel)", "", false, "0.0.0+unknown"),
		)
	})
	When("a build version was explicitly embedded", func() {
		It("uses that version", func() {
			Expect(Current("2.0.0+abcdef.dirty")).To(Equal("2.0.0+abcdef.dirty"))
		})
	})
})
