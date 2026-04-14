package addons

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// Sidecar is a running addon sidecar process plus an HTTP client scoped to
// its Unix domain socket. One Sidecar corresponds to one addon's
// <addonDir>/sock endpoint; concurrent HTTP calls are safe because the
// client is reused.
type Sidecar struct {
	Name         string
	AddonDir     string
	SocketPath   string
	Binary       string
	HealthPath   string
	StartTimeout time.Duration

	cmd     *exec.Cmd
	client  *http.Client
	logFile *os.File
}

// SidecarVersion is the response shape for GET /version.
type SidecarVersion struct {
	Version         string `json:"version"`
	ProtocolVersion int    `json:"protocol_version"`
}

// NewSidecar builds a Sidecar from an addon directory and the parsed manifest
// SidecarSpec. Defaults (health path, start timeout, socket filename) are
// filled in from the spec or hard-coded fallbacks.
func NewSidecar(addonDir string, spec SidecarSpec) *Sidecar {
	socket := spec.Socket
	if socket == "" {
		socket = "sock"
	}
	if !filepath.IsAbs(socket) {
		socket = filepath.Join(addonDir, socket)
	}

	health := spec.HealthPath
	if health == "" {
		health = "/health"
	}

	start := time.Duration(spec.StartTimeoutSeconds) * time.Second
	if start <= 0 {
		start = 10 * time.Second
	}

	binary := spec.Binary
	if binary != "" && !filepath.IsAbs(binary) {
		// Resolve the binary path using the same logic as sidecarBinaryPath
		// in lifecycle_helpers.go so integrity hashing and spawning agree:
		// prefer the manifest-declared relative path, fall back to the
		// common addonDir/bin/<name> layout.
		primary := filepath.Join(addonDir, binary)
		if info, err := os.Lstat(primary); err == nil && info.Mode().IsRegular() {
			binary = primary
		} else {
			binary = filepath.Join(addonDir, "bin", binary)
		}
	}

	s := &Sidecar{
		AddonDir:     addonDir,
		SocketPath:   socket,
		Binary:       binary,
		HealthPath:   health,
		StartTimeout: start,
	}
	s.client = &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", s.SocketPath)
			},
			DisableKeepAlives: false,
		},
	}
	return s
}

// Start spawns the sidecar binary and waits up to StartTimeout for /health to
// return 200. A missing Binary is a configuration error; a non-responsive
// process is a timeout.
func (s *Sidecar) Start(ctx context.Context) error {
	if s.Binary == "" {
		return fmt.Errorf("sidecar %q: no binary configured", s.Name)
	}
	// Use plain exec.Command (NOT CommandContext) so the child process's
	// lifetime is NOT tied to the start-operation context. Callers
	// (Reconciler, Lifecycle) pass a timeout-bounded ctx to limit how
	// long they wait for /health; if we bound cmd's lifetime to that
	// ctx, the defer cancel() in the caller kills the child the moment
	// Start returns successfully. The child must live until explicit
	// Stop() (graceful) or process exit.
	cmd := exec.Command(s.Binary)
	cmd.Dir = s.AddonDir
	cmd.Env = append(cmd.Environ(),
		"ADDON_DIR="+s.AddonDir,
		"ADDON_SOCKET="+s.SocketPath,
	)
	// Capture the sidecar's stdout+stderr to a log file so operators can
	// diagnose crashes and so reconciler logs surface addon-side errors.
	// The log file lives next to the addon install to keep everything
	// scoped to one directory. Append mode so multiple restarts stack.
	logPath := filepath.Join(s.AddonDir, "sidecar.log")
	if lf, lerr := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); lerr == nil {
		cmd.Stdout = lf
		cmd.Stderr = lf
		s.logFile = lf
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("sidecar %q: spawning %s: %w", s.Name, s.Binary, err)
	}
	s.cmd = cmd

	deadline := time.Now().Add(s.StartTimeout)
	for time.Now().Before(deadline) {
		if err := s.Health(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			_ = s.killProcess()
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	_ = s.killProcess()
	return fmt.Errorf("sidecar %q: /health did not return 200 within %s; check %s",
		s.Name, s.StartTimeout, s.SocketPath)
}

// Stop tries graceful shutdown over HTTP, then SIGTERM, then SIGKILL. Returns
// nil if the process is already gone.
func (s *Sidecar) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(shutdownCtx, http.MethodPost, s.url("/shutdown"), nil)
	if err == nil {
		if resp, derr := s.client.Do(req); derr == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}

	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}

	// Wait up to 5s for the process to exit on its own.
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
	}

	// SIGTERM, wait another 5s.
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
	}

	// SIGKILL as the last resort.
	if err := s.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("sidecar %q: SIGKILL: %w", s.Name, err)
	}
	<-done
	return nil
}

// Health issues GET /health and returns an error if the status is not 200.
func (s *Sidecar) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url(s.HealthPath), nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health: status %d from %s", resp.StatusCode, s.HealthPath)
	}
	return nil
}

// Version returns the sidecar's reported version and protocol version.
func (s *Sidecar) Version(ctx context.Context) (*SidecarVersion, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url("/version"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sidecar %q: GET /version: %w", s.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sidecar %q: GET /version: status %d", s.Name, resp.StatusCode)
	}
	var v SidecarVersion
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, fmt.Errorf("sidecar %q: decoding /version: %w", s.Name, err)
	}
	return &v, nil
}

// Hook POSTs a JSON-encoded event payload to /hook/<event>. Non-2xx responses
// become errors; 2xx responses are drained and discarded.
func (s *Sidecar) Hook(ctx context.Context, event string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sidecar %q: marshaling hook %q payload: %w", s.Name, event, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url("/hook/"+event), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sidecar %q: POST /hook/%s: %w", s.Name, event, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sidecar %q: hook %q returned status %d", s.Name, event, resp.StatusCode)
	}
	return nil
}

// Proxy reverse-proxies an incoming HTTP request to the sidecar over its Unix
// socket. Used by core to expose /addons/<name>/ui/* from the sciclaw web UI.
func (s *Sidecar) Proxy(w http.ResponseWriter, r *http.Request) {
	target, _ := url.Parse("http://addon")
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = s.client.Transport
	rp.ServeHTTP(w, r)
}

func (s *Sidecar) url(path string) string {
	if path == "" || path[0] != '/' {
		path = "/" + path
	}
	return "http://addon" + path
}

func (s *Sidecar) killProcess() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	_ = s.cmd.Process.Kill()
	_, _ = s.cmd.Process.Wait()
	return nil
}
