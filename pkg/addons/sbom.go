package addons

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// SBOM (Software Bill of Materials) is a machine-readable record of an
// addon's installed identity suitable for export, audit, and reproducibility
// checks. It combines the registry's pinned-commit / hash fields with the
// manifest's declared dependencies so a single document captures every
// piece of information an auditor needs to reason about trust.
type SBOM struct {
	Name              string           `json:"name"`
	Version           string           `json:"version"`
	Source            string           `json:"source"`
	InstalledCommit   string           `json:"installed_commit"`
	InstalledAt       string           `json:"installed_at"`
	ManifestSHA256    string           `json:"manifest_sha256"`
	BootstrapSHA256   string           `json:"bootstrap_sha256,omitempty"`
	SidecarSHA256     string           `json:"sidecar_sha256,omitempty"`
	SignedTag         string           `json:"signed_tag,omitempty"`
	SignatureVerified bool             `json:"signature_verified"`
	Track             string           `json:"track,omitempty"`
	Dependencies      []SBOMDependency `json:"dependencies,omitempty"`
	Platform          string           `json:"platform"`
	ExportedAt        string           `json:"exported_at"`
}

// SBOMDependency describes an external requirement declared by the addon.
// Kind is one of "sciclaw", "runtime", or "platform"; Value holds a version
// constraint, binary name, or platform identifier depending on Kind.
type SBOMDependency struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Export computes an SBOM for a single installed addon. It loads the
// addon's registry entry from store and merges it with the supplied
// manifest to produce a full dependency listing. An error is returned if
// the addon is not present in the registry, or if any required argument
// is nil.
//
// platform is the host platform the SBOM is being exported on (e.g.,
// "linux/amd64"). The now callback is invoked once to stamp ExportedAt
// so tests can pin the timestamp deterministically.
func Export(store *Store, manifest *Manifest, name, platform string, now func() time.Time) (*SBOM, error) {
	if store == nil {
		return nil, fmt.Errorf("sbom Export: store is nil")
	}
	if manifest == nil {
		return nil, fmt.Errorf("sbom Export: manifest is nil")
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("sbom Export: addon name must be non-empty")
	}
	if now == nil {
		now = time.Now
	}

	entry, err := store.Get(name)
	if err != nil {
		return nil, fmt.Errorf("sbom Export: loading registry entry for %q: %w", name, err)
	}
	if entry == nil {
		return nil, fmt.Errorf("sbom Export: addon %q not found in registry", name)
	}

	sbom := &SBOM{
		Name:              name,
		Version:           entry.Version,
		Source:            entry.Source,
		InstalledCommit:   entry.InstalledCommit,
		InstalledAt:       entry.InstalledAt,
		ManifestSHA256:    entry.ManifestSHA256,
		BootstrapSHA256:   entry.BootstrapSHA256,
		SidecarSHA256:     entry.SidecarSHA256,
		SignatureVerified: entry.SignatureVerified,
		Platform:          platform,
		ExportedAt:        now().UTC().Format(time.RFC3339),
	}
	if entry.SignedTag != nil {
		sbom.SignedTag = *entry.SignedTag
	}
	if entry.Track != nil {
		sbom.Track = *entry.Track
	}

	// Dependency enumeration: one entry per declared input. Sciclaw
	// version constraint always comes first so the JSON reads
	// top-to-bottom in the natural "host first, then runtime, then
	// platform" order.
	deps := make([]SBOMDependency, 0,
		1+len(manifest.Requires.Runtime)+len(manifest.Requires.Platform))
	if strings.TrimSpace(manifest.Requires.Sciclaw) != "" {
		deps = append(deps, SBOMDependency{
			Kind:  "sciclaw",
			Value: manifest.Requires.Sciclaw,
		})
	}
	for _, bin := range manifest.Requires.Runtime {
		if strings.TrimSpace(bin) == "" {
			continue
		}
		deps = append(deps, SBOMDependency{Kind: "runtime", Value: bin})
	}
	for _, p := range manifest.Requires.Platform {
		if strings.TrimSpace(p) == "" {
			continue
		}
		deps = append(deps, SBOMDependency{Kind: "platform", Value: p})
	}
	if len(deps) > 0 {
		sbom.Dependencies = deps
	}

	return sbom, nil
}

// WriteSBOM writes the SBOM to w as indented JSON (two-space indent) followed
// by a trailing newline. It returns any I/O error from w.Write.
func WriteSBOM(w io.Writer, sbom *SBOM) error {
	if w == nil {
		return fmt.Errorf("WriteSBOM: writer is nil")
	}
	if sbom == nil {
		return fmt.Errorf("WriteSBOM: sbom is nil")
	}
	data, err := json.MarshalIndent(sbom, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling SBOM: %w", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("writing SBOM: %w", err)
	}
	return nil
}
