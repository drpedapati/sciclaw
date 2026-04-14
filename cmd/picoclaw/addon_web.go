package main

// addon_web.go implements Wave 4b of the sciClaw addon system: the web
// backend endpoints that surface enabled addons to the frontend tab bar and
// reverse-proxy addon UIs from their sidecars over Unix sockets.
//
// Two endpoints are exposed:
//
//   GET /api/addons/enabled      — JSON array of enabled addons (name, version,
//                                   state, ui_tab). Consumed by web/src/addons/
//                                   AddonTabs.tsx to render the dynamic tab
//                                   list on the sciClaw web UI.
//
//   /addons/<name>/ui/*          — Reverse proxy to the named addon's sidecar
//                                   over its Unix socket. The sidecar serves
//                                   its own React/Vue/HTML bundle from /ui/*.
//
// The endpoints rely on the package-level addonSidecarRegistry installed by
// main() at startup (see addon_hooks.go). When no registry is installed (tests,
// one-shot CLI), the proxy endpoint degrades to 503 rather than panicking.
//
// Security note: the web UI currently has no per-user session auth — every
// request to 127.0.0.1:4142 is effectively trusted. If/when sciClaw grows a
// session layer, this handler should propagate the authenticated user via an
// X-Sciclaw-User header so addons can authorise without re-implementing auth.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/addons"
	"github.com/sipeed/picoclaw/pkg/logger"
)

// addonEnabledEntry is the JSON shape returned by GET /api/addons/enabled.
// It mirrors the Addon interface in web/src/api/addons.ts so the frontend
// deserialises without additional adapter logic.
type addonEnabledEntry struct {
	Name    string                  `json:"name"`
	Version string                  `json:"version"`
	State   string                  `json:"state"`
	UITab   *addonEnabledEntryUITab `json:"ui_tab,omitempty"`
}

type addonEnabledEntryUITab struct {
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
	Path string `json:"path,omitempty"`
}

// addonWebHome is overridden in tests to point at a temp registry. Production
// callers leave it nil so the handler falls back to the real sciclawHomeDir().
// Using a function indirection (rather than a string var) keeps the test
// seam minimal without requiring callers to pass state through the webServer
// struct.
var addonWebHome func() string

func resolveAddonHome() string {
	if addonWebHome != nil {
		return addonWebHome()
	}
	return sciclawHomeDir()
}

// handleAddonsEnabled serves GET /api/addons/enabled. Consumed by the web UI
// tab bar (Wave 4c) and polled every 30s, so a missing store or a single bad
// manifest must degrade gracefully rather than 500 the whole call.
func (s *webServer) handleAddonsEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	home := resolveAddonHome()
	store := addons.NewStore(home)
	reg, err := store.Load()
	if err != nil {
		logger.WarnCF("web", "addon registry load failed", map[string]interface{}{
			"error": err.Error(),
			"path":  store.Path(),
		})
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse manifests fresh on every call. This endpoint is not hot — the
	// frontend polls it at 30s cadence — so caching would add complexity
	// without material benefit, and would risk staleness after enable/disable.
	out := make([]addonEnabledEntry, 0, len(reg.Addons))
	for name, entry := range reg.Addons {
		if entry == nil || entry.State != addons.StateEnabled {
			continue
		}
		manifestPath := filepath.Join(home, "addons", name, "addon.json")
		m, perr := addons.ParseManifest(manifestPath)
		if perr != nil {
			// One bad manifest must not poison the whole response. Log and
			// skip — the user will see the addon missing from the tab list
			// and can debug via `sciclaw addon status <name>`.
			logger.WarnCF("web", "addon manifest parse failed; skipping", map[string]interface{}{
				"addon": name,
				"path":  manifestPath,
				"error": perr.Error(),
			})
			continue
		}
		e := addonEnabledEntry{
			Name:    name,
			Version: entry.Version,
			State:   string(entry.State),
		}
		if m.Provides.UITab != nil {
			e.UITab = &addonEnabledEntryUITab{
				Name: m.Provides.UITab.Name,
				Icon: m.Provides.UITab.Icon,
				Path: m.Provides.UITab.Path,
			}
		}
		out = append(out, e)
	}

	// Deterministic order so the frontend tab list is stable across polls.
	sortAddonEnabled(out)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// sortAddonEnabled sorts the slice in place by name. A small bespoke sort
// avoids pulling sort.Slice into a handler that is not performance critical
// and keeps the alloc profile predictable.
func sortAddonEnabled(xs []addonEnabledEntry) {
	for i := 1; i < len(xs); i++ {
		j := i
		for j > 0 && xs[j-1].Name > xs[j].Name {
			xs[j-1], xs[j] = xs[j], xs[j-1]
			j--
		}
	}
}

// handleAddonProxy reverse-proxies requests to /addons/<name>/ui/* to the
// named addon's sidecar over its Unix socket. The <name> segment is extracted
// and validated (no traversal, no absolute paths, no hidden names); the
// /addons/<name> prefix is stripped before delegating to sidecar.Proxy so the
// addon sees its own root path (/ui/index.html, etc).
//
// The sidecar's proxy is backed by httputil.ReverseProxy, which transparently
// handles HTTP Upgrade requests — WebSockets (needed by selkies-gstreamer in
// the webtop addon) work without special-casing here.
func (s *webServer) handleAddonProxy(w http.ResponseWriter, r *http.Request) {
	// Parse /addons/<name>/<rest>.
	rest := strings.TrimPrefix(r.URL.Path, "/addons/")
	if rest == r.URL.Path {
		// Prefix did not match. Should not happen because the mux only
		// routes /addons/ here, but guard against misconfiguration.
		jsonErr(w, "not an addon path", http.StatusNotFound)
		return
	}

	name, subpath := splitAddonPath(rest)
	if err := validateAddonName(name); err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Try the in-process registry first. This is populated in gatewayCmd()
	// where sidecars are actually spawned. In the web-server-only process
	// (sciclaw web) the registry is nil, so fall through to the disk lookup.
	var side *addons.Sidecar
	if addonSidecarRegistry != nil {
		side = addonSidecarRegistry.Lookup(name)
	}
	if side == nil {
		// Disk fallback: the web server and the gateway are separate
		// processes. The gateway owns the sidecar lifecycle, but the web
		// server only needs the socket path to proxy through. Read the
		// persisted registry to confirm the addon is enabled, parse the
		// manifest, and construct a client Sidecar on demand.
		resolved, err := lookupSidecarOnDisk(name)
		if err != nil {
			logger.WarnCF("web", "addon proxy disk lookup failed", map[string]interface{}{
				"addon": name,
				"error": err.Error(),
			})
			jsonErr(w, fmt.Sprintf("addon %q not running: %s", name, err.Error()), http.StatusServiceUnavailable)
			return
		}
		side = resolved
	}

	// Rewrite r.URL.Path so the sidecar sees its own root. A nil URL or a
	// URL with a different RawPath is not a concern here: net/http always
	// populates URL.Path from the request line, and we only mutate Path
	// (leaving RawQuery and the original r.URL pointer alone so logging
	// further up the stack still works).
	if !strings.HasPrefix(subpath, "/") {
		subpath = "/" + subpath
	}
	newURL := *r.URL
	newURL.Path = subpath
	newURL.RawPath = ""
	newReq := r.Clone(r.Context())
	newReq.URL = &newURL
	newReq.RequestURI = "" // Required when sending an outbound request.

	side.Proxy(w, newReq)
}

// lookupSidecarOnDisk constructs a client Sidecar for an already-running
// addon by reading persisted state instead of going through the in-process
// SidecarRegistry. It is used by handleAddonProxy when the web server runs
// in a different process from the gateway (sciclaw web vs sciclaw gateway):
// the gateway owns the sidecar process and its SidecarRegistry, but the web
// server only needs the socket path to reverse-proxy UI traffic.
//
// Returns an error if:
//   - the addon is not registered at all
//   - the addon is registered but not enabled (caller should 503)
//   - the manifest is missing/unparseable
//   - the socket file does not exist (sidecar not running, or the gateway
//     has not reconciled it yet)
func lookupSidecarOnDisk(name string) (*addons.Sidecar, error) {
	home := sciclawHomeDir()
	store := addons.NewStore(home)
	entry, err := store.Get(name)
	if err != nil {
		return nil, fmt.Errorf("reading registry: %w", err)
	}
	if entry == nil {
		return nil, fmt.Errorf("addon %q not installed", name)
	}
	if entry.State != addons.StateEnabled {
		return nil, fmt.Errorf("addon %q is %s (not enabled)", name, entry.State)
	}
	addonDir := filepath.Join(home, "addons", name)
	manifest, err := addons.ParseManifest(filepath.Join(addonDir, "addon.json"))
	if err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	side := addons.NewSidecar(addonDir, manifest.Sidecar)
	side.Name = name
	if _, serr := os.Stat(side.SocketPath); serr != nil {
		return nil, fmt.Errorf("socket %s: %w", side.SocketPath, serr)
	}
	return side, nil
}

// splitAddonPath splits "name/rest/of/path" into ("name", "/rest/of/path").
// Empty rest becomes "/". A missing slash returns ("name", "/").
func splitAddonPath(rest string) (string, string) {
	// Trim leading slash if present (defensive — TrimPrefix already handled
	// the "/addons/" prefix but a double-slash request could still leave
	// one).
	rest = strings.TrimLeft(rest, "/")
	if rest == "" {
		return "", "/"
	}
	i := strings.Index(rest, "/")
	if i < 0 {
		return rest, "/"
	}
	return rest[:i], rest[i:]
}

// validateAddonName rejects any name that could escape the addon directory
// or shadow a hidden file. The check is deliberately strict: addon names are
// already constrained to lowercase alphanumeric + hyphen by the manifest
// validator, so anything weirder is a sign of an attack or misconfiguration.
func validateAddonName(name string) error {
	if name == "" {
		return fmt.Errorf("addon name is required")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid addon name %q", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("invalid addon name %q", name)
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("invalid addon name %q", name)
	}
	// Rejecting NUL and path separators covers every OS-level traversal
	// vector we care about; manifest validation handles whitespace and
	// case upstream, so we don't re-enforce it here.
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("invalid addon name %q", name)
	}
	return nil
}
