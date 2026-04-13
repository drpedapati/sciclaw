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

// Validate checks that all required addon.json fields are present and safe.
func (m *Manifest) Validate() error {
	if err := ValidateAddonName(m.Name); err != nil {
		return err
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
	if err := validateBootstrapPath(m.Bootstrap.Install, "bootstrap.install"); err != nil {
		return err
	}
	if err := validateBootstrapPath(m.Bootstrap.Uninstall, "bootstrap.uninstall"); err != nil {
		return err
	}
	return nil
}

// ValidateAddonName enforces a safe character set on addon names so that
// filesystem paths, shell arguments, and URL segments derived from the name
// cannot be used for traversal or injection.
//
// The accepted pattern is: [a-z0-9] followed by [a-z0-9._-]{0,63}.
// Names must not begin with '.' (blocks "." and ".." as names) or contain
// path separators, NUL, or other metacharacters.
func ValidateAddonName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("addon name is required")
	}
	if len(name) > 64 {
		return fmt.Errorf("addon name %q too long (max 64 chars)", name)
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
			if i == 0 && r == '.' {
				return fmt.Errorf("addon name %q must not start with '.'", name)
			}
		default:
			return fmt.Errorf("addon name %q contains invalid character %q (allowed: a-z 0-9 . _ -)", name, r)
		}
	}
	return nil
}

// validateBootstrapPath rejects bootstrap script paths that contain shell
// metacharacters or absolute/traversal segments. Bootstrap paths are
// resolved relative to the addon directory and executed directly (not via
// shell), but we still reject obviously hostile inputs at parse time so
// that manifest review catches them.
func validateBootstrapPath(path, field string) error {
	if path == "" {
		return nil
	}
	if strings.ContainsAny(path, "\x00\n\r;&|`$<>*?(){}[]\"'\\") {
		return fmt.Errorf("addon.json %s %q contains shell metacharacter", field, path)
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "..") || strings.Contains(path, "/..") {
		return fmt.Errorf("addon.json %s %q must be a relative path inside the addon directory", field, path)
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
	if len(parts) != 3 {
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
