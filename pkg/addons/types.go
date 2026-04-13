// Package addons is the sciClaw addon system data plane: manifest parsing,
// registry storage, integrity hashing, and git-ref resolution.
//
// Later waves layer lifecycle, sidecar HTTP, hook dispatch, UI/CLI proxies,
// and signature verification on top of these primitives.
package addons

// State is the lifecycle state of an addon as tracked in the registry.
// Phase 1 only distinguishes installed vs enabled — disabled addons are
// represented as installed per RFC section 2.
type State string

const (
	StateInstalled State = "installed"
	StateEnabled   State = "enabled"
)

// Manifest is the parsed form of an addon.json file.
//
// Required fields: Name, Version, Requires.Sciclaw, Sidecar.Binary.
// All other fields are optional and may be zero.
type Manifest struct {
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Description string       `json:"description,omitempty"`
	Author      string       `json:"author,omitempty"`
	Homepage    string       `json:"homepage,omitempty"`
	Requires    Requirements `json:"requires"`
	Sidecar     SidecarSpec  `json:"sidecar"`
	Provides    Provides     `json:"provides,omitempty"`
	Bootstrap   Bootstrap    `json:"bootstrap,omitempty"`
	Compose     string       `json:"compose,omitempty"`
}

// Requirements describes what the addon needs from the host.
type Requirements struct {
	Sciclaw  string   `json:"sciclaw"`
	Runtime  []string `json:"runtime,omitempty"`
	Platform []string `json:"platform,omitempty"`
}

// SidecarSpec describes how core launches and health-checks the addon sidecar.
type SidecarSpec struct {
	Binary              string `json:"binary"`
	Socket              string `json:"socket,omitempty"`
	StartTimeoutSeconds int    `json:"start_timeout_seconds,omitempty"`
	HealthPath          string `json:"health_path,omitempty"`
}

// Provides declares the extension points this addon contributes.
type Provides struct {
	UITab        *UITab   `json:"ui_tab,omitempty"`
	CLIGroup     string   `json:"cli_group,omitempty"`
	Hooks        []string `json:"hooks,omitempty"`
	ConfigSchema string   `json:"config_schema,omitempty"`
}

// UITab describes a tab the addon injects into the core web UI.
type UITab struct {
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
	Path string `json:"path,omitempty"`
}

// Bootstrap holds paths to optional install/uninstall scripts or directories.
type Bootstrap struct {
	Install   string `json:"install,omitempty"`
	Uninstall string `json:"uninstall,omitempty"`
}

// RegistryEntry is one addon's persisted record in registry.json.
//
// Nullable fields (Track, SignedTag, PreviousCommit) use pointers so that
// null and empty-string are distinguishable in the JSON form.
type RegistryEntry struct {
	Version           string  `json:"version"`
	InstalledAt       string  `json:"installed_at"`
	InstalledCommit   string  `json:"installed_commit"`
	ManifestSHA256    string  `json:"manifest_sha256"`
	BootstrapSHA256   string  `json:"bootstrap_sha256,omitempty"`
	SidecarSHA256     string  `json:"sidecar_sha256,omitempty"`
	State             State   `json:"state"`
	Source            string  `json:"source"`
	Track             *string `json:"track"`
	SignedTag         *string `json:"signed_tag"`
	SignatureVerified bool    `json:"signature_verified"`
	PreviousCommit    *string `json:"previous_commit"`
}

// Registry is the top-level shape of registry.json.
type Registry struct {
	Version int                       `json:"version"`
	Addons  map[string]*RegistryEntry `json:"addons"`
}
