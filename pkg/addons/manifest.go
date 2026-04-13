package addons

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ParseManifest reads and validates an addon.json file at path.
func ParseManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading addon manifest %s: %w", path, err)
	}
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		// Retry without strict mode so forward-compat fields don't break parsing.
		if err2 := json.Unmarshal(data, &m); err2 != nil {
			return nil, fmt.Errorf("parsing addon manifest %s: %w", path, err2)
		}
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("invalid addon manifest %s: %w", path, err)
	}
	return &m, nil
}

// Validate checks that all required addon.json fields are present.
func (m *Manifest) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("addon.json missing required field: name")
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("addon.json missing required field: version")
	}
	if strings.TrimSpace(m.Requires.Sciclaw) == "" {
		return fmt.Errorf("addon.json missing required field: requires.sciclaw")
	}
	if strings.TrimSpace(m.Sidecar.Binary) == "" {
		return fmt.Errorf("addon.json missing required field: sidecar.binary")
	}
	return nil
}

// ValidateRequirements checks that the host satisfies the addon's
// requires.sciclaw, requires.platform, and requires.runtime constraints.
//
// lookPath is injected so tests can stub binary lookup; production callers
// pass exec.LookPath.
func ValidateRequirements(m *Manifest, sciclawVersion, platform string, lookPath func(string) (string, error)) error {
	if err := checkVersionConstraint(m.Requires.Sciclaw, sciclawVersion); err != nil {
		return fmt.Errorf("addon %q: %w; upgrade sciclaw or install a compatible addon version", m.Name, err)
	}
	if len(m.Requires.Platform) > 0 {
		if !containsString(m.Requires.Platform, platform) {
			return fmt.Errorf("addon %q does not support platform %q (supported: %s)",
				m.Name, platform, strings.Join(m.Requires.Platform, ", "))
		}
	}
	for _, bin := range m.Requires.Runtime {
		if strings.TrimSpace(bin) == "" {
			continue
		}
		if _, err := lookPath(bin); err != nil {
			return fmt.Errorf("addon %q requires %q on PATH; install it and retry", m.Name, bin)
		}
	}
	return nil
}

func containsString(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

// checkVersionConstraint supports ">=X.Y.Z" and exact "X.Y.Z" only.
// A bespoke parser avoids adding a semver dependency for a tiny subset.
func checkVersionConstraint(constraint, actual string) error {
	constraint = strings.TrimSpace(constraint)
	actual = strings.TrimSpace(actual)
	if constraint == "" {
		return nil
	}
	if actual == "" {
		return fmt.Errorf("cannot satisfy version constraint %q: sciclaw version is empty", constraint)
	}
	op := "="
	rhs := constraint
	if strings.HasPrefix(constraint, ">=") {
		op = ">="
		rhs = strings.TrimSpace(constraint[2:])
	}

	want, err := parseSemver(rhs)
	if err != nil {
		return fmt.Errorf("unparseable version constraint %q: %w", constraint, err)
	}
	got, err := parseSemver(actual)
	if err != nil {
		return fmt.Errorf("unparseable sciclaw version %q: %w", actual, err)
	}

	cmp := compareSemver(got, want)
	switch op {
	case ">=":
		if cmp < 0 {
			return fmt.Errorf("requires sciclaw %s, have %s", constraint, actual)
		}
	case "=":
		if cmp != 0 {
			return fmt.Errorf("requires sciclaw %s, have %s", constraint, actual)
		}
	}
	return nil
}

// parseSemver parses a "X.Y.Z" version (leading "v" tolerated, pre-release
// and build suffixes ignored). Phase 1 only needs a total order on core
// releases; a full semver impl belongs in a later wave if at all.
func parseSemver(s string) ([3]int, error) {
	var out [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	// Drop pre-release / build metadata.
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, fmt.Errorf("expected X.Y.Z, got %q", s)
	}
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return out, fmt.Errorf("non-numeric segment %q in %q", parts[i], s)
		}
		if n < 0 {
			return out, fmt.Errorf("negative segment in %q", s)
		}
		out[i] = n
	}
	return out, nil
}

func compareSemver(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
