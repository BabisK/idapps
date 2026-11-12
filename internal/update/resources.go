package update

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BabisK/idapps/internal/catalogue"
)

type Identity struct {
	Host         string `json:"host"`
	Installation string `json:"installation"`
}
type ServiceRecord struct {
	App         string        `json:"app"`
	Engine      string        `json:"engine"`
	ContainerID string        `json:"containerID"`
	Definition  catalogue.App `json:"definition"`
}
type resourceFile struct {
	Host         string                   `json:"host"`
	Installation string                   `json:"installation"`
	Services     map[string]ServiceRecord `json:"services"`
}

func (s *Store) Identity() (Identity, error) {
	file := filepath.Join(s.Paths.Config, "identity.json")
	var id Identity
	e := readJSON(file, &id)
	if os.IsNotExist(e) {
		var b [16]byte
		if _, e = rand.Read(b[:]); e != nil {
			return id, e
		}
		id = Identity{s.Host, hex.EncodeToString(b[:])}
		if e = writeJSON(file, id); e != nil {
			return id, e
		}
	} else if e != nil {
		return id, e
	}
	if id.Host != s.Host || len(id.Installation) != 32 {
		return id, fmt.Errorf("invalid or foreign installation identity")
	}
	if _, e = hex.DecodeString(id.Installation); e != nil {
		return id, fmt.Errorf("invalid installation identity")
	}
	return id, nil
}
func (s *Store) Resources() (Identity, map[string]ServiceRecord, error) {
	id, e := s.Identity()
	if e != nil {
		return id, nil, e
	}
	file := filepath.Join(s.Paths.Config, "resources.json")
	var r resourceFile
	e = readJSON(file, &r)
	if os.IsNotExist(e) {
		return id, map[string]ServiceRecord{}, nil
	}
	if e != nil {
		return id, nil, e
	}
	if r.Host != s.Host || r.Installation != id.Installation || r.Services == nil {
		return id, nil, fmt.Errorf("invalid or foreign service records")
	}
	for name, rec := range r.Services {
		if !validName(name) || rec.App != name || rec.Definition.Kind != "service" || rec.Engine == "" || rec.Definition.ContainerName == "" {
			return id, nil, fmt.Errorf("invalid service record %q", name)
		}
		if err := catalogue.ValidateServiceDefinition(name, rec.Definition); err != nil {
			return id, nil, fmt.Errorf("invalid service record %q: %w", name, err)
		}
	}
	return id, r.Services, nil
}
func (s *Store) SaveResources(id Identity, records map[string]ServiceRecord) error {
	if id.Host != s.Host || id.Installation == "" {
		return fmt.Errorf("invalid installation identity")
	}
	return writeJSON(filepath.Join(s.Paths.Config, "resources.json"), resourceFile{id.Host, id.Installation, records})
}
func sameIdentity(a, b catalogue.App) bool {
	if a.Kind != b.Kind || a.ContainerName != b.ContainerName {
		return false
	}
	vols := func(a catalogue.App) []string {
		var v []string
		for _, m := range a.Mounts {
			if m.Type == "volume" {
				v = append(v, m.Name)
			}
		}
		sort.Strings(v)
		return v
	}
	x, y := vols(a), vols(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
