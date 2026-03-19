package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewWebServerServesEmbeddedApp(t *testing.T) {
	srv := newWebServer(nil, "")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for embedded app, got %d", rec.Code)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "<!doctype html>") {
		t.Fatalf("expected embedded index.html, got body %q", rec.Body.String())
	}
}

func TestNewWebServerSpaFallbackUsesEmbeddedIndex(t *testing.T) {
	srv := newWebServer(nil, "")

	req := httptest.NewRequest(http.MethodGet, "/routing", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for SPA fallback, got %d", rec.Code)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "<!doctype html>") {
		t.Fatalf("expected embedded SPA index.html, got body %q", rec.Body.String())
	}
}
