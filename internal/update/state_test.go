package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/BabisK/idapps/internal/catalogue"
	"github.com/BabisK/idapps/internal/storage"
	"github.com/BabisK/idapps/internal/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func testStore(t GinkgoTInterface) (*Store, *Generation) {
	t.Helper()
	dir := t.TempDir()
	s, e := NewStore(storage.Paths{Cache: filepath.Join(dir, "cache", "idapps", "catalogues"), Config: filepath.Join(dir, "config", "idapps"), Bin: filepath.Join(dir, "data", "idapps", "bin")}, "1.0.0")
	Expect(e).NotTo(HaveOccurred())

	files, e := testutil.CatalogueFiles("../../catalogue")
	Expect(e).NotTo(HaveOccurred())
	b, e := testutil.CatalogueArchive(files)
	Expect(e).NotTo(HaveOccurred())

	h := sha256.Sum256(b)
	return s, &Generation{Commit: "0123456789abcdef0123456789abcdef01234567", SHA256: hex.EncodeToString(h[:]), Bytes: b}
}

var _ = Describe("update reconciliation counts", func() {
	It("reports initial, unchanged, repaired and catalogue changes", func() {
		store, generation := testStore(GinkgoT())
		executable := filepath.Join(GinkgoT().TempDir(), "idapps")
		activate := func(g *Generation) Reconciliation {
			var result Reconciliation
			err := store.WithLock(context.Background(), func() error {
				var err error
				result, err = store.ActivateWithResult(g, executable)
				return err
			})
			Expect(err).NotTo(HaveOccurred())
			return result
		}
		Expect(activate(generation)).To(Equal(Reconciliation{Added: 22}))
		Expect(activate(generation)).To(Equal(Reconciliation{Unchanged: 22}))
		Expect(os.Remove(store.linkPath("psql"))).To(Succeed())
		files, err := testutil.CatalogueFiles("../../catalogue")
		Expect(err).NotTo(HaveOccurred())
		files["repo/catalogue/new-aws/app.json"] = files["repo/catalogue/aws/app.json"]
		delete(files, "repo/catalogue/aws/app.json")
		data, err := testutil.CatalogueArchive(files)
		Expect(err).NotTo(HaveOccurred())
		sum := sha256.Sum256(data)
		next := &Generation{Commit: strings.Repeat("b", 40), SHA256: hex.EncodeToString(sum[:]), Bytes: data}
		Expect(activate(next)).To(Equal(Reconciliation{Added: 1, Removed: 1, Repaired: 1, Unchanged: 20}))
	})
})

var _ = Describe("activating a catalogue", func() {
	var store *Store
	var generation *Generation
	var exe string

	BeforeEach(func() {
		store, generation = testStore(GinkgoT())
		exe = filepath.Join(GinkgoT().TempDir(), "idapps")
	})

	When("the catalogue is activated for the first time", func() {
		It("makes the applications available in the active snapshot", func() {
			var apps int
			err := store.WithLock(context.Background(), func() error {
				if err := store.Activate(generation, exe); err != nil {
					return err
				}
				current, _, err := store.Snapshot()
				if err == nil {
					apps = len(current.Apps)
				}
				return err
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(apps).To(Equal(22))
		})
	})

	When("an app link is missing or occupied", func() {
		var link string
		BeforeEach(func() {
			Expect(store.WithLock(context.Background(), func() error { return store.Activate(generation, exe) })).To(Succeed())
			link = store.linkPath("psql")
			Expect(os.Remove(link)).To(Succeed())
		})

		It("repairs a missing owned link", func() {
			Expect(store.WithLock(context.Background(), func() error { return store.Activate(generation, exe) })).To(Succeed())
			target, err := os.Readlink(link)
			Expect(err).NotTo(HaveOccurred())
			Expect(target).To(Equal(exe))
		})

		It("preserves an unrelated file at the link path", func() {
			Expect(os.WriteFile(link, []byte("foreign"), 0600)).To(Succeed())
			Expect(store.WithLock(context.Background(), func() error { return store.Activate(generation, exe) })).NotTo(Succeed())
			data, err := os.ReadFile(link)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("foreign"))
		})
	})
})

var _ = Describe("recovering an interrupted catalogue update", func() {
	When("the interrupted update changed an owned link", func() {
		It("restores the prior generation after an interrupted update", func() {
			t := GinkgoT()
			s, g := testStore(t)
			exe := filepath.Join(t.TempDir(), "idapps")
			{
				e := s.WithLock(context.Background(), func() error { return s.Activate(g, exe) })
				Expect(e).NotTo(HaveOccurred())
			}

			old, e := s.loadState()
			Expect(e).NotTo(HaveOccurred())

			next := state{Host: s.Host, Commit: old.Commit, Hash: old.Hash, Links: map[string]string{"psql": exe + "-new"}}
			{
				e = writeJSON(filepath.Join(s.Paths.Config, "journal.json"), journal{s.Host, old, next})
				Expect(e).NotTo(HaveOccurred())
			}

			p := s.linkPath("psql")
			{
				e = os.Remove(p)
				Expect(e).NotTo(HaveOccurred())
			}
			{

				e = os.Symlink(exe+"-new", p)
				Expect(e).NotTo(HaveOccurred())
			}
			{

				e = s.WithLock(context.Background(), func() error { return nil })
				Expect(e).NotTo(HaveOccurred())
			}

			target, e := os.Readlink(p)
			Expect(e != nil || target != exe).To(BeFalse(), target, e)

		})
	})
	When("a link was retargeted outside idapps", func() {
		It("refuses to overwrite an externally retargeted link", func() {
			t := GinkgoT()
			s, g := testStore(t)
			exe := filepath.Join(t.TempDir(), "idapps")
			{
				e := s.WithLock(context.Background(), func() error { return s.Activate(g, exe) })
				Expect(e).NotTo(HaveOccurred())
			}

			old, e := s.loadState()
			Expect(e).NotTo(HaveOccurred())

			next := state{Host: s.Host, Commit: old.Commit, Hash: old.Hash, Links: map[string]string{"psql": exe + "-new"}}
			{
				e = writeJSON(filepath.Join(s.Paths.Config, "journal.json"), journal{s.Host, old, next})
				Expect(e).NotTo(HaveOccurred())
			}

			p := s.linkPath("psql")
			{
				e = os.Remove(p)
				Expect(e).NotTo(HaveOccurred())
			}
			{

				e = os.Symlink("/foreign", p)
				Expect(e).NotTo(HaveOccurred())
			}
			{

				e = s.WithLock(context.Background(), func() error { return nil })
				Expect(e).To(HaveOccurred(), "accepted retargeted link")
			}
			{

				v, e := os.Readlink(p)
				Expect(e != nil || v != filepath.Clean("/foreign")).To(BeFalse(), v, e)
			}

		})
	})
})

var _ = Describe("catalogue storage", func() {
	When("cached catalogue data was removed", func() {
		It("retains links and requests an update after cache removal", func() {
			t := GinkgoT()
			s, g := testStore(t)
			exe := filepath.Join(t.TempDir(), "idapps")
			{
				e := s.WithLock(context.Background(), func() error { return s.Activate(g, exe) })
				Expect(e).NotTo(HaveOccurred())
			}
			{

				e := os.Remove(filepath.Join(s.Paths.Cache, g.SHA256+".zip"))
				Expect(e).NotTo(HaveOccurred())
			}

			e := s.WithLock(context.Background(), func() error { _, _, err := s.Snapshot(); return err })
			Expect(e == nil || !strings.Contains(e.Error(), "run idapps update")).To(BeFalse(), e)
			{

				target, e := os.Readlink(s.linkPath("psql"))
				Expect(e != nil || target != exe).To(BeFalse(), target, e)
			}
			{

				e = s.WithLock(context.Background(), func() error { return s.Activate(g, exe) })
				Expect(e).NotTo(HaveOccurred())
			}

		})
	})
	When("a managed directory is a symlink", func() {
		It("rejects symlinks inside managed storage", func() {
			t := GinkgoT()
			base := t.TempDir()
			root := filepath.Join(base, "idapps")
			{
				e := os.Mkdir(root, 0700)
				Expect(e).NotTo(HaveOccurred())
			}

			outside := t.TempDir()
			if e := os.Symlink(outside, filepath.Join(root, "catalogues")); e != nil {
				t.Skipf("symlink unavailable: %v", e)
			}
			{
				e := secureDir(filepath.Join(root, "catalogues"))
				Expect(e).To(HaveOccurred(), "accepted symlink inside managed root")
			}

		})
	})
})

var _ = Describe("applying catalogue changes", func() {
	When("apps and image versions change", func() {
		var store *Store
		var exe, foreign string
		var next *Generation
		var originalHash [sha256.Size]byte

		BeforeEach(func() {
			var generation *Generation
			store, generation = testStore(GinkgoT())
			exe = filepath.Join(GinkgoT().TempDir(), "idapps")
			Expect(os.WriteFile(exe, []byte("unchanged-launcher"), 0700)).To(Succeed())
			before, err := os.ReadFile(exe)
			Expect(err).NotTo(HaveOccurred())
			originalHash = sha256.Sum256(before)
			Expect(store.WithLock(context.Background(), func() error { return store.Activate(generation, exe) })).To(Succeed())

			files, err := testutil.CatalogueFiles("../../catalogue")
			Expect(err).NotTo(HaveOccurred())
			retiredData := files["repo/catalogue/redis/app.json"]
			var definition catalogue.App
			Expect(json.Unmarshal(retiredData, &definition)).To(Succeed())
			Expect(store.WithLock(context.Background(), func() error {
				id, records, err := store.Resources()
				if err != nil {
					return err
				}
				records["redis"] = ServiceRecord{App: "redis", Engine: "test-engine", ContainerID: "recorded-container", Definition: definition}
				return store.SaveResources(id, records)
			})).To(Succeed())

			files["repo/catalogue/new-psql/app.json"] = files["repo/catalogue/psql/app.json"]
			delete(files, "repo/catalogue/psql/app.json")
			delete(files, "repo/catalogue/redis/app.json")
			var client map[string]any
			Expect(json.Unmarshal(files["repo/catalogue/redis-cli/app.json"], &client)).To(Succeed())
			client["image"].(map[string]any)["tag"] = "0.0.0-fixture"
			files["repo/catalogue/redis-cli/app.json"], err = json.Marshal(client)
			Expect(err).NotTo(HaveOccurred())
			nextBytes, err := testutil.CatalogueArchive(files)
			Expect(err).NotTo(HaveOccurred())
			hash := sha256.Sum256(nextBytes)
			next = &Generation{Commit: strings.Repeat("b", 40), SHA256: hex.EncodeToString(hash[:]), Bytes: nextBytes}
			foreign = filepath.Join(store.Paths.Bin, "unrelated")
			Expect(os.WriteFile(foreign, []byte("user data"), 0600)).To(Succeed())
			Expect(store.WithLock(context.Background(), func() error { return store.Activate(next, exe) })).To(Succeed())
		})

		It("removes old links and creates links for new applications", func() {
			for _, name := range []string{"psql", "redis"} {
				_, err := os.Lstat(store.linkPath(name))
				Expect(os.IsNotExist(err)).To(BeTrue(), name)
			}
			target, err := os.Readlink(store.linkPath("new-psql"))
			Expect(err).NotTo(HaveOccurred())
			Expect(target).To(Equal(exe))
		})

		It("activates the changed image version", func() {
			var current *catalogue.Catalogue
			var commit string
			err := store.WithLock(context.Background(), func() error {
				var err error
				current, commit, err = store.Snapshot()
				return err
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(commit).To(Equal(next.Commit))
			Expect(current.Apps["redis-cli"].Image.Tag).To(Equal("0.0.0-fixture"))
		})

		It("retains records for retired services", func() {
			var record ServiceRecord
			err := store.WithLock(context.Background(), func() error {
				_, records, err := store.Resources()
				if err == nil {
					record = records["redis"]
				}
				return err
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(record.ContainerID).To(Equal("recorded-container"))
		})

		It("leaves unrelated files and the launcher unchanged", func() {
			out, err := os.ReadFile(foreign)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(out)).To(Equal("user data"))
			after, err := os.ReadFile(exe)
			Expect(err).NotTo(HaveOccurred())
			Expect(sha256.Sum256(after)).To(Equal(originalHash))
		})
	})
	When("the launcher has moved", func() {
		It("rebinds only owned links after the launcher moves", func() {
			t := GinkgoT()
			s, g := testStore(t)
			dir := t.TempDir()
			oldExe := filepath.Join(dir, "old-idapps")
			newExe := filepath.Join(dir, "new-idapps")
			{
				err := os.WriteFile(oldExe, []byte("launcher"), 0700)
				Expect(err).NotTo(HaveOccurred())
			}
			{

				err := s.WithLock(context.Background(), func() error { return s.Activate(g, oldExe) })
				Expect(err).NotTo(HaveOccurred())
			}

			foreign := filepath.Join(s.Paths.Bin, "foreign-link")
			{
				err := os.Symlink(oldExe, foreign)
				Expect(err).NotTo(HaveOccurred())
			}
			{

				err := os.Rename(oldExe, newExe)
				Expect(err).NotTo(HaveOccurred())
			}
			{

				err := s.WithLock(context.Background(), func() error { _, _, e := s.Snapshot(); return e })
				Expect(err).NotTo(HaveOccurred())
			}
			{

				target, err := os.Readlink(s.linkPath("psql"))
				Expect(err != nil || target != oldExe).To(BeFalse(), "ordinary snapshot changed link: %q %v", target, err)
			}
			{

				err := s.WithLock(context.Background(), func() error { return s.Activate(g, newExe) })
				Expect(err).NotTo(HaveOccurred())
			}
			{

				target, err := os.Readlink(s.linkPath("psql"))
				Expect(err != nil || target != newExe).To(BeFalse(), "owned link not reconciled: %q %v", target, err)
			}
			{

				target, err := os.Readlink(foreign)
				Expect(err != nil || target != oldExe).To(BeFalse(), "foreign link changed: %q %v", target, err)
			}
			{

				b, err := os.ReadFile(newExe)
				Expect(err != nil || string(b) != "launcher").To(BeFalse(), "executable changed: %q %v", b, err)
			}

		})
	})
})

var _ = Describe("failed catalogue transactions", func() {
	When("an existing generation is being replaced", func() {
		DescribeTable("restores the previous generation if the transaction fails", func(point string) {
			t := GinkgoT()
			s, g := testStore(t)
			oldExe := filepath.Join(t.TempDir(), "old")
			{
				e := s.WithLock(context.Background(), func() error { return s.Activate(g, oldExe) })
				Expect(e).NotTo(HaveOccurred())
			}

			prior := transactionFault
			DeferCleanup(func() { transactionFault = prior })
			transactionFault = func(p string) error {
				if p == point {
					return fmt.Errorf("injected fault")
				}
				return nil
			}
			newExe := filepath.Join(t.TempDir(), "new")
			{
				e := s.WithLock(context.Background(), func() error { return s.Activate(g, newExe) })
				Expect(e).To(HaveOccurred(), "fault was not reached")
			}

			for _, n := range []string{"aws", "psql", "redis"} {
				v, e := os.Readlink(s.linkPath(n))
				Expect(e).NotTo(HaveOccurred(), n)
				Expect(v).To(Equal(oldExe), n)
			}
			st, e := s.loadState()
			Expect(e).NotTo(HaveOccurred())
			Expect(st.Hash).To(Equal(g.SHA256))
		},
			Entry("when writing the journal", "journal"),
			Entry("when creating the first link", "link:aws"),
			Entry("when creating a later link", "link:psql"),
			Entry("when activating the catalogue", "activation"),
		)
	})
	When("the first generation cannot be activated", func() {
		It("leaves no active catalogue when the first update fails", func() {
			t := GinkgoT()
			s, g := testStore(t)
			prior := transactionFault
			defer func() { transactionFault = prior }()
			transactionFault = func(p string) error {
				if p == "link:psql" {
					return fmt.Errorf("injected fault")
				}
				return nil
			}
			e := s.WithLock(context.Background(), func() error { return s.Activate(g, filepath.Join(t.TempDir(), "idapps")) })
			Expect(e).To(HaveOccurred(), "expected fault")
			{

				_, e = os.Lstat(s.linkPath("aws"))
				Expect(!os.IsNotExist(e)).To(BeFalse(), "partial link remains", e)
			}

			st, e := s.loadState()
			Expect(e != nil || st.Hash != "" || len(st.Links) != 0).To(BeFalse(), st, e)

		})
	})
})

var _ = Describe("protecting update state", func() {
	When("another update holds the lock", func() {
		It("honours cancellation without entering the protected operation", func() {
			store, _ := testStore(GinkgoT())
			entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			var releaseOnce sync.Once
			unlock := func() { releaseOnce.Do(func() { close(release) }) }
			DeferCleanup(unlock)
			go func() {
				finished <- store.WithLock(context.Background(), func() error { close(entered); <-release; return nil })
			}()
			Eventually(entered, 5*time.Second).Should(BeClosed())
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			called := false
			err := store.WithLock(ctx, func() error { called = true; return nil })
			Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue(), "%v", err)
			Expect(called).To(BeFalse())
			unlock()
			Eventually(finished, 5*time.Second).Should(Receive(Succeed()))
			Expect(store.WithLock(context.Background(), func() error { return nil })).To(Succeed())
		})
	})
	When("a journal was copied from another host", func() {
		It("leaves the journal, active generation and owned links untouched", func() {
			store, generation := testStore(GinkgoT())
			exe := filepath.Join(GinkgoT().TempDir(), "idapps")
			Expect(store.WithLock(context.Background(), func() error { return store.Activate(generation, exe) })).To(Succeed())
			old, err := store.loadState()
			Expect(err).NotTo(HaveOccurred())
			path := filepath.Join(store.Paths.Config, "journal.json")
			Expect(writeJSON(path, journal{Host: "another-host", Old: old, New: old})).To(Succeed())
			original, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			called := false
			err = store.WithLock(context.Background(), func() error { called = true; return nil })
			Expect(err).To(MatchError(ContainSubstring("another host")))
			Expect(called).To(BeFalse())
			contents, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(contents).To(Equal(original))
			target, err := os.Readlink(store.linkPath("psql"))
			Expect(err).NotTo(HaveOccurred())
			Expect(target).To(Equal(exe))
			active, err := store.loadState()
			Expect(err).NotTo(HaveOccurred())
			Expect(active).To(Equal(old))
		})
	})
	When("an interrupted update affects an owned link", func() {
		for _, app := range []string{"aws", "etcd", "etcdctl", "go", "helm", "helmfile", "kubectl", "mariadb-server", "memcached", "mysql", "mysql-server", "mysqldump", "nats", "nats-server", "pg_dump", "pgadmin4", "phpmyadmin", "postgres", "psql", "redis", "redis-cli", "redis-stack"} {
			app := app
			It("restores the previous generation after changing "+app, func() {
				store, generation := testStore(GinkgoT())
				oldExe := filepath.Join(GinkgoT().TempDir(), "old")
				newExe := filepath.Join(GinkgoT().TempDir(), "new")
				Expect(store.WithLock(context.Background(), func() error { return store.Activate(generation, oldExe) })).To(Succeed())
				prior := transactionFault
				DeferCleanup(func() { transactionFault = prior })
				faultReached := false
				transactionFault = func(point string) error {
					if point == "link:"+app {
						faultReached = true
						return fmt.Errorf("injected interruption at %s", point)
					}
					return nil
				}
				err := store.WithLock(context.Background(), func() error { return store.Activate(generation, newExe) })
				Expect(err).To(MatchError(ContainSubstring("injected interruption")))
				Expect(faultReached).To(BeTrue())
				active, err := store.loadState()
				Expect(err).NotTo(HaveOccurred())
				Expect(active.Hash).To(Equal(generation.SHA256))
				for name := range active.Links {
					target, readErr := os.Readlink(store.linkPath(name))
					Expect(readErr).NotTo(HaveOccurred(), name)
					Expect(target).To(Equal(oldExe), name)
				}
				_, err = os.Lstat(filepath.Join(store.Paths.Config, "journal.json"))
				Expect(os.IsNotExist(err)).To(BeTrue(), "%v", err)
			})
		}
	})
})

var _ = Describe("archive generation integrity", func() {
	It("detects a corrupted cached archive", func() {
		store, generation := testStore(GinkgoT())
		exe := filepath.Join(GinkgoT().TempDir(), "idapps")
		Expect(store.WithLock(context.Background(), func() error { return store.Activate(generation, exe) })).To(Succeed())
		zipPath := filepath.Join(store.Paths.Cache, generation.SHA256+".zip")
		Expect(os.WriteFile(zipPath, []byte("corrupt"), 0600)).To(Succeed())
		_, _, err := store.Snapshot()
		Expect(err).To(MatchError(ContainSubstring("corrupt")))
	})
})

var _ = Describe("archive transaction rollback", func() {
	for _, point := range []string{"journal", "link:aws", "activation"} {
		point := point
		It("restores the prior generation and links after "+point, func() {
			store, old := testStore(GinkgoT())
			oldExe := filepath.Join(GinkgoT().TempDir(), "old")
			newExe := filepath.Join(GinkgoT().TempDir(), "new")
			files := map[string][]byte{}
			Expect(filepath.WalkDir("../../catalogue", func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				rel, e := filepath.Rel("../../catalogue", p)
				if e != nil {
					return e
				}
				files["repo/catalogue/"+filepath.ToSlash(rel)], e = os.ReadFile(p)
				return e
			})).To(Succeed())
			data := archiveFixture(files)
			hash := sha256.Sum256(data)
			next := &Generation{Commit: strings.Repeat("b", 40), SHA256: hex.EncodeToString(hash[:]), Bytes: data}
			Expect(store.WithLock(context.Background(), func() error { return store.Activate(old, oldExe) })).To(Succeed())
			prior := transactionFault
			DeferCleanup(func() { transactionFault = prior })
			reached := false
			transactionFault = func(p string) error {
				if p == point {
					reached = true
					return fmt.Errorf("injected interruption")
				}
				return nil
			}
			Expect(store.WithLock(context.Background(), func() error { return store.Activate(next, newExe) })).To(MatchError(ContainSubstring("injected interruption")))
			Expect(reached).To(BeTrue())
			st, err := store.loadState()
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Hash).To(Equal(old.SHA256))
			for name := range st.Links {
				target, err := os.Readlink(store.linkPath(name))
				Expect(err).NotTo(HaveOccurred())
				Expect(target).To(Equal(oldExe))
			}
			_, commit, err := store.Snapshot()
			Expect(err).NotTo(HaveOccurred())
			Expect(commit).To(Equal(old.Commit))
		})
	}
})

var _ = Describe("existing generation cache safety", func() {
	It("rejects an oversized existing archive before activation", func() {
		store, g := testStore(GinkgoT())
		Expect(secureDir(store.Paths.Cache)).To(Succeed())
		f, err := os.Create(filepath.Join(store.Paths.Cache, g.SHA256+".zip"))
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Truncate(maxArchiveBytes + 1)).To(Succeed())
		Expect(f.Close()).To(Succeed())
		err = store.WithLock(context.Background(), func() error { return store.Activate(g, filepath.Join(GinkgoT().TempDir(), "idapps")) })
		Expect(err).To(MatchError(ContainSubstring("oversized")))
	})
})
