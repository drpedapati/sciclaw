package addons

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Store is a goroutine-safe reader/writer for the addon registry file at
// <sciclawHome>/addons/registry.json. Writes are atomic (temp file + rename),
// matching the pkg/profile convention.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore returns a registry store rooted at sciclawHome.
func NewStore(sciclawHome string) *Store {
	return &Store{
		path: filepath.Join(sciclawHome, "addons", "registry.json"),
	}
}

// Path returns the on-disk location of registry.json.
func (s *Store) Path() string { return s.path }

// Load reads the registry from disk. A missing file is not an error; it
// returns a zero-value Registry so callers can treat the cold-start and
// empty-state paths identically.
func (s *Store) Load() (*Registry, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{Version: 1, Addons: map[string]*RegistryEntry{}}, nil
		}
		return nil, fmt.Errorf("reading addon registry %s: %w", s.path, err)
	}
	var r Registry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parsing addon registry %s: %w; fix or delete the file", s.path, err)
	}
	if r.Addons == nil {
		r.Addons = map[string]*RegistryEntry{}
	}
	if r.Version == 0 {
		r.Version = 1
	}
	return &r, nil
}

// Save writes the registry to disk atomically.
func (s *Store) Save(r *Registry) error {
	if r == nil {
		return fmt.Errorf("cannot save nil registry")
	}
	if r.Addons == nil {
		r.Addons = map[string]*RegistryEntry{}
	}
	if r.Version == 0 {
		r.Version = 1
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating addon registry directory %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling addon registry: %w", err)
	}
	data = append(data, '\n')

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp registry %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp registry into place: %w", err)
	}
	return nil
}

// Get returns the registry entry for name, or (nil, nil) if not present.
func (s *Store) Get(name string) (*RegistryEntry, error) {
	r, err := s.Load()
	if err != nil {
		return nil, err
	}
	entry, ok := r.Addons[name]
	if !ok {
		return nil, nil
	}
	return entry, nil
}

// Set upserts an addon's registry entry. Other entries are preserved.
func (s *Store) Set(name string, entry *RegistryEntry) error {
	if name == "" {
		return fmt.Errorf("addon name must be non-empty")
	}
	if entry == nil {
		return fmt.Errorf("addon %q: entry must be non-nil", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.Load()
	if err != nil {
		return err
	}
	r.Addons[name] = entry
	return s.Save(r)
}

// Delete removes an addon's entry. No-op if the entry does not exist.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.Load()
	if err != nil {
		return err
	}
	if _, ok := r.Addons[name]; !ok {
		return nil
	}
	delete(r.Addons, name)
	return s.Save(r)
}

// List returns all addon names sorted lexicographically.
func (s *Store) List() ([]string, error) {
	r, err := s.Load()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(r.Addons))
	for name := range r.Addons {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
