package hookpolicy

import "path/filepath"

// LoadPolicy loads and merges workspace HOOKS.md and hooks.yaml.
// Precedence: hooks.yaml overrides HOOKS.md.
func LoadPolicy(workspace string) (Policy, Diagnostics, error) {
	policy := defaultPolicy()
	diag := Diagnostics{}

	if err := parseNaturalLanguagePolicy(workspace, &policy, &diag); err != nil {
		return Policy{}, diag, err
	}
	if err := parseYAMLOverrides(workspace, &policy, &diag); err != nil {
		return Policy{}, diag, err
	}

	// Normalize relative audit path for callers.
	if policy.AuditPath != "" && !filepath.IsAbs(policy.AuditPath) {
		policy.AuditPath = filepath.Join(workspace, policy.AuditPath)
	}

	return policy, diag, nil
}
