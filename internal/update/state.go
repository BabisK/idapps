package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/BabisK/idapps/internal/catalogue"
	"github.com/BabisK/idapps/internal/storage"
	"github.com/gofrs/flock"
)

var safeName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validName(n string) bool {
	if !safeName.MatchString(n) {
		return false
	}
	switch n {
	case "idapps", "update", "run", "list", "version", "help", "con", "prn", "aux", "nul":
		return false
	}
	for _, p := range []string{"com", "lpt"} {
		if len(n) == 4 && strings.HasPrefix(n, p) && n[3] >= '1' && n[3] <= '9' {
			return false
		}
	}
	return true
}

type state struct {
	Host   string            `json:"host"`
	Commit string            `json:"commit"`
	Hash   string            `json:"hash"`
	Links  map[string]string `json:"links"`
}
type journal struct {
	Host string `json:"host"`
	Old  state  `json:"old"`
	New  state  `json:"new"`
}

var transactionFault = func(string) error { return nil }

type Store struct {
	Paths   storage.Paths
	Host    string
	Version string
}

func NewStore(p storage.Paths, version string) (*Store, error) {
	machine, e := machineID()
	if e != nil || strings.TrimSpace(machine) == "" {
		return nil, fmt.Errorf("cannot establish host identity: %w", e)
	}
	hash := sha256.Sum256([]byte(machine))
	return &Store{p, hex.EncodeToString(hash[:]), version}, nil
}

// WithLock serializes updates, recovery and snapshots. It does not hold the lock while an app runs.
func (s *Store) WithLock(ctx context.Context, f func() error) error {
	if err := secureDir(s.Paths.Config); err != nil {
		return err
	}
	lockPath := filepath.Join(s.Paths.Config, "update.lock")
	if info, err := os.Lstat(lockPath); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("unsafe update lock %s", lockPath)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	l := flock.New(lockPath)
	ok, e := l.TryLockContext(ctx, 100*time.Millisecond)
	if e != nil {
		return fmt.Errorf("lock update state: %w", e)
	}
	if !ok {
		return fmt.Errorf("cannot acquire update lock")
	}
	defer l.Unlock()
	if e = s.recover(); e != nil {
		return e
	}
	return f()
}
func secureDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("managed directory must be absolute: %s", dir)
	}
	dir = filepath.Clean(dir)
	// The OS base directory may legitimately traverse a symlink (for example /var on macOS).
	// Ownership checks start at the idapps directory and include every descendant.
	root := dir
	for filepath.Base(root) != "idapps" {
		parent := filepath.Dir(root)
		if parent == root {
			return fmt.Errorf("invalid managed directory %s", dir)
		}
		root = parent
	}
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	parts := []string{root}
	if rel != "." {
		current := root
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			parts = append(parts, current)
		}
	}
	for _, p := range parts {
		info, e := os.Lstat(p)
		if os.IsNotExist(e) {
			if e = os.Mkdir(p, 0700); e != nil && !os.IsExist(e) {
				return e
			}
			info, e = os.Lstat(p)
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("unsafe managed directory %s", p)
		}
	}
	return nil
}

func atomicWrite(file string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(file), ".idapps-*")
	if e != nil {
		return e
	}
	temp := f.Name()
	defer os.Remove(temp)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = os.Rename(temp, file); e != nil {
		return e
	}
	return syncDir(filepath.Dir(file))
}
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func readJSON(file string, v any) error {
	info, e := os.Lstat(file)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsafe state file %s", file)
	}
	b, e := os.ReadFile(file)
	if e != nil {
		return e
	}
	if e = catalogue.CheckJSON(b); e != nil {
		return fmt.Errorf("invalid state file %s: %w", file, e)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		return e
	}
	if e = d.Decode(new(any)); e != io.EOF {
		return fmt.Errorf("invalid trailing state")
	}
	return nil
}
func writeJSON(file string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return atomicWrite(file, b)
}
func (s *Store) validState(st state) error {
	if st.Host != s.Host {
		return fmt.Errorf("state belongs to another host (%q)", st.Host)
	}
	if st.Links == nil {
		return fmt.Errorf("invalid link ownership record")
	}
	if (st.Commit == "") != (st.Hash == "") || st.Commit != "" && (!commitPattern.MatchString(st.Commit) || !hashPattern.MatchString(st.Hash)) {
		return fmt.Errorf("invalid active catalogue reference")
	}
	for n, t := range st.Links {
		if !validName(n) || t == "" || (!filepath.IsAbs(t) && (filepath.Base(t) != t || t == "." || t == ".." || strings.ContainsAny(t, `/\`))) {
			return fmt.Errorf("invalid owned link record for %q", n)
		}
	}
	return nil
}
func (s *Store) loadState() (state, error) {
	st := state{}
	e := readJSON(filepath.Join(s.Paths.Config, "state.json"), &st)
	if os.IsNotExist(e) {
		return state{Host: s.Host, Links: map[string]string{}}, nil
	}
	if e != nil {
		return st, fmt.Errorf("read state: %w", e)
	}
	if e = s.validState(st); e != nil {
		return st, e
	}
	return st, nil
}
func (s *Store) linkPath(n string) string {
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return filepath.Join(s.Paths.Bin, n)
}
func linkValue(p string) (string, bool, error) {
	info, e := os.Lstat(p)
	if os.IsNotExist(e) {
		return "", false, nil
	}
	if e != nil {
		return "", false, e
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", true, fmt.Errorf("foreign path %s", p)
	}
	v, e := os.Readlink(p)
	return v, true, e
}
func (s *Store) recover() error {
	var j journal
	e := readJSON(filepath.Join(s.Paths.Config, "journal.json"), &j)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return fmt.Errorf("read update journal: %w", e)
	}
	if j.Host != s.Host {
		return fmt.Errorf("update journal belongs to another host")
	}
	if e = s.validState(j.Old); e != nil {
		return e
	}
	if e = s.validState(j.New); e != nil {
		return e
	}
	if e = secureDir(s.Paths.Bin); e != nil {
		return e
	}
	if e = s.restore(j.Old, j.New); e != nil {
		return fmt.Errorf("recover update: %w; resolve the affected path and retry", e)
	}
	if e = writeJSON(filepath.Join(s.Paths.Config, "state.json"), j.Old); e != nil {
		return e
	}
	if e = os.Remove(filepath.Join(s.Paths.Config, "journal.json")); e != nil {
		return e
	}
	return syncDir(s.Paths.Config)
}
func (s *Store) restore(old, new state) error {
	keys := map[string]bool{}
	for n := range old.Links {
		keys[n] = true
	}
	for n := range new.Links {
		keys[n] = true
	}
	for n := range keys {
		p := s.linkPath(n)
		v, exists, e := linkValue(p)
		if e != nil {
			return e
		}
		ov, had := old.Links[n]
		nv := new.Links[n]
		if exists && v != ov && v != nv {
			return fmt.Errorf("foreign or retargeted link %s", p)
		}
		if exists && had && v == ov {
			continue
		}
		if exists {
			if e = os.Remove(p); e != nil {
				return e
			}
		}
		if had {
			if e = os.Symlink(ov, p); e != nil {
				return fmt.Errorf("restore %s: %w", p, e)
			}
		}
	}
	return syncDir(s.Paths.Bin)
}

// Reconciliation summarizes the app-link changes made by an update.
type Reconciliation struct {
	Added, Removed, Repaired, Unchanged int
}

// ActivateWithResult activates a generation and reports its link changes. The
// caller must hold the store lock throughout planning and activation.
func (s *Store) ActivateWithResult(g *Generation, executable string) (Reconciliation, error) {
	var result Reconciliation
	old, err := s.loadState()
	if err != nil {
		return result, err
	}
	c, err := parseGeneration(g.Bytes, s.Version)
	if err != nil {
		return result, err
	}
	target := executable
	if filepath.Clean(filepath.Dir(executable)) == filepath.Clean(s.Paths.Bin) {
		target = filepath.Base(executable)
	}
	for name := range c.Apps {
		if _, owned := old.Links[name]; !owned {
			result.Added++
			continue
		}
		current, exists, err := linkValue(s.linkPath(name))
		if err != nil {
			return Reconciliation{}, err
		}
		if !exists || current != target {
			result.Repaired++
		} else {
			result.Unchanged++
		}
	}
	for name := range old.Links {
		if _, exists := c.Apps[name]; !exists {
			result.Removed++
		}
	}
	if err := s.Activate(g, executable); err != nil {
		return Reconciliation{}, err
	}
	return result, nil
}

// Activate reconciles owned links and the active reference under the store lock.
func (s *Store) Activate(g *Generation, executable string) error {
	if !filepath.IsAbs(executable) {
		return fmt.Errorf("launcher path must be absolute")
	}
	if !commitPattern.MatchString(g.Commit) || !hashPattern.MatchString(g.SHA256) {
		return fmt.Errorf("invalid catalogue revision")
	}
	sum := sha256.Sum256(g.Bytes)
	if hex.EncodeToString(sum[:]) != g.SHA256 {
		return fmt.Errorf("catalogue hash mismatch")
	}
	c, e := parseGeneration(g.Bytes, s.Version)
	if e != nil {
		return e
	}
	if e = secureDir(s.Paths.Cache); e != nil {
		return e
	}
	if e = secureDir(s.Paths.Bin); e != nil {
		return e
	}
	old, e := s.loadState()
	if e != nil {
		return e
	}
	_, records, e := s.Resources()
	if e != nil {
		return e
	}
	for name, rec := range records {
		if app, ok := c.Apps[name]; ok && !sameIdentity(rec.Definition, app) {
			return fmt.Errorf("%s has managed resources with a different identity; stop and clean them first", name)
		}
	}

	target := executable
	if filepath.Clean(filepath.Dir(executable)) == filepath.Clean(s.Paths.Bin) {
		target = filepath.Base(executable)
	}
	next := state{Host: s.Host, Commit: g.Commit, Hash: g.SHA256, Links: map[string]string{}}
	for n := range c.Apps {
		next.Links[n] = target
	}
	keys := map[string]bool{}
	for n := range old.Links {
		keys[n] = true
	}
	for n := range next.Links {
		keys[n] = true
	}
	names := make([]string, 0, len(keys))
	for n := range keys {
		names = append(names, n)
	}
	sort.Strings(names)
	changed := false
	for _, n := range names {
		p := s.linkPath(n)
		v, exists, e := linkValue(p)
		if e != nil {
			return e
		}
		want, desired := next.Links[n]
		if exists != desired || (exists && desired && v != want) {
			changed = true
		}
		if exists {
			oldTarget, owned := old.Links[n]
			if !owned || v != oldTarget {
				return fmt.Errorf("foreign or retargeted link %s", p)
			}
		}
	}
	generationPath := filepath.Join(s.Paths.Cache, g.SHA256+".zip")
	cached, readErr := readCache(generationPath, maxArchiveBytes)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	if !bytes.Equal(cached, g.Bytes) {
		if e = atomicWrite(generationPath, g.Bytes); e != nil {
			return e
		}
	}

	if !changed && old.Commit == next.Commit && old.Hash == next.Hash {
		return nil
	}
	j := journal{s.Host, old, next}
	if e = writeJSON(filepath.Join(s.Paths.Config, "journal.json"), j); e != nil {
		return e
	}
	e = transactionFault("journal")
	for _, n := range names {
		if e != nil {
			break
		}
		p := s.linkPath(n)
		v, exists, err := linkValue(p)
		if err != nil {
			e = err
			break
		}
		want, desired := next.Links[n]
		if exists && desired && v == want {
			continue
		}
		if exists {
			if v != old.Links[n] {
				e = fmt.Errorf("link changed during update: %s", p)
				break
			}
			if e = os.Remove(p); e != nil {
				break
			}
		}
		if desired {
			if e = os.Symlink(want, p); e != nil {
				e = fmt.Errorf("create symlink %s (check symlink permissions): %w", p, e)
				break
			}
		}
		e = transactionFault("link:" + n)
	}
	if e == nil {
		e = syncDir(s.Paths.Bin)
	}
	if e == nil {
		e = writeJSON(filepath.Join(s.Paths.Config, "state.json"), next)
	}
	if e == nil {
		e = transactionFault("activation")
	}
	if e != nil {
		if rollback := s.recover(); rollback != nil {
			return errors.Join(e, rollback)
		}
		return e
	}
	if e = os.Remove(filepath.Join(s.Paths.Config, "journal.json")); e != nil {
		return e
	}
	return syncDir(s.Paths.Config)
}

// Snapshot verifies the active cached bytes and returns a validated immutable generation.
func (s *Store) Snapshot() (*catalogue.Catalogue, string, error) {
	st, e := s.loadState()
	if e != nil {
		return nil, "", e
	}
	if st.Hash == "" {
		return nil, "", fmt.Errorf("no catalogue installed; run idapps update")
	}

	b, e := readCache(filepath.Join(s.Paths.Cache, st.Hash+".zip"), maxArchiveBytes)
	if e != nil {
		return nil, st.Commit, fmt.Errorf("catalogue cache unavailable; run idapps update: %w", e)
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != st.Hash {
		return nil, st.Commit, fmt.Errorf("catalogue cache corrupt; run idapps update")
	}
	c, e := parseArchiveMode(b, s.Version, false)
	if e != nil {
		return nil, st.Commit, fmt.Errorf("cached catalogue invalid: %w", e)
	}
	return c, st.Commit, nil
}

func parseGeneration(b []byte, version string) (*catalogue.Catalogue, error) {
	return parseArchive(b, version)
}

func readCache(file string, limit int64) ([]byte, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("unsafe or oversized cache file")
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		return nil, fmt.Errorf("oversized cache file")
	}
	return b, err
}
