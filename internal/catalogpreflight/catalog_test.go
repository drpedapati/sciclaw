// Package catalogpreflight contains test-only experiments, not runtime code.
package catalogpreflight

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type model struct {
	Slug       string `json:"slug"`
	Visibility string `json:"visibility"`
	Minimum    string `json:"minimal_client_version"`
	Default    string `json:"default_reasoning_level"`
	Efforts    []struct {
		Effort string `json:"effort"`
	} `json:"supported_reasoning_levels"`
}

// Three-component versions are compared numerically, not lexically.
func compatible(minimum, client string) bool {
	if minimum == "" {
		return true
	}
	a, b := strings.Split(minimum, "."), strings.Split(client, ".")
	if len(a) != 3 || len(b) != 3 {
		return false
	}
	for i := range a {
		x, errX := strconv.Atoi(a[i])
		y, errY := strconv.Atoi(b[i])
		if errX != nil || errY != nil || x < 0 || y < 0 {
			return false
		}
		if x != y {
			return x < y
		}
	}
	return true
}

func fetch(ctx context.Context, client *http.Client, base, token, account, version string) ([]model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/models?client_version="+version, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("ChatGPT-Account-ID", account)
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("version", version)
	req.Header.Set("User-Agent", "codex_cli_rs/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog HTTP %d", resp.StatusCode)
	}
	const limit = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(raw) > limit {
		return nil, fmt.Errorf("catalog response too large or unreadable")
	}
	var payload struct {
		Models []model `json:"models"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil, fmt.Errorf("invalid catalog JSON")
	}
	var selected []model
	seen := map[string]bool{}
	for _, m := range payload.Models {
		if m.Slug == "" || m.Visibility != "list" || !compatible(m.Minimum, version) || seen[m.Slug] {
			continue
		}
		seen[m.Slug] = true
		selected = append(selected, m)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no selectable models")
	}
	return selected, nil
}

func TestCatalogRequestAndSelection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.URL.Query().Get("client_version") != "0.160.0" {
			t.Error("incorrect catalog URL")
		}
		for k, want := range map[string]string{"Authorization": "Bearer synthetic-token", "ChatGPT-Account-ID": "synthetic-account", "originator": "codex_cli_rs", "version": "0.160.0", "User-Agent": "codex_cli_rs/0.160.0"} {
			if r.Header.Get(k) != want {
				t.Errorf("missing identity header %s", k)
			}
		}
		fmt.Fprint(w, `{"models":[{"slug":"gpt-6.1-sol","visibility":"list","minimal_client_version":"0.153.0","default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"max"}]},{"slug":"hidden","visibility":"hide"},{"slug":"future","visibility":"list","minimal_client_version":"0.161.0"},{"slug":"gpt-6.1-sol","visibility":"list"}]}`)
	}))
	defer srv.Close()
	got, err := fetch(t.Context(), srv.Client(), srv.URL, "synthetic-token", "synthetic-account", "0.160.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Slug != "gpt-6.1-sol" || got[0].Default != "low" || len(got[0].Efforts) != 3 || got[0].Efforts[1].Effort != "medium" {
		t.Fatalf("metadata lost or filtering incorrect: %#v", got)
	}
}

func TestCatalogFailuresAreBoundedAndSanitized(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, "synthetic-secret"}, {"rate-limit", 429, "synthetic-secret"}, {"invalid", 200, "synthetic-secret"}, {"empty", 200, `{"models":[]}`}, {"oversized", 200, strings.Repeat("x", (1<<20)+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer srv.Close()
			_, err := fetch(t.Context(), srv.Client(), srv.URL, "synthetic-secret", "account", "0.160.0")
			if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("expected sanitized failure, got %v", err)
			}
		})
	}
	t.Run("deadline", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer srv.Close()
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		if _, err := fetch(ctx, srv.Client(), srv.URL, "token", "account", "0.160.0"); err == nil {
			t.Fatal("deadline not enforced")
		}
	})
}

func TestVersionGateUsesNumericComponents(t *testing.T) {
	for _, tc := range []struct {
		min, client string
		want        bool
	}{{"0.153.0", "0.144.1", false}, {"0.153.0", "0.160.0", true}, {"0.9.0", "0.10.0", true}, {"bad", "0.160.0", false}, {"0.160.0", "0.160.0", true}} {
		if got := compatible(tc.min, tc.client); got != tc.want {
			t.Errorf("compatible(%q,%q)=%v", tc.min, tc.client, got)
		}
	}
}
