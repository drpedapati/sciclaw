package auth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type credentialTransport func(*http.Request) (*http.Response, error)

func (f credentialTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestValidOpenAICredentialRefreshAndPreservation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cred := &AuthCredential{AccessToken: "old", RefreshToken: "refresh", AccountID: "account", AuthMethod: "oauth", Provider: "openai", ExpiresAt: time.Now().Add(-time.Minute)}
	if err := SetCredential("openai", cred); err != nil {
		t.Fatal(err)
	}
	original := sharedAuthClient
	t.Cleanup(func() { sharedAuthClient = original })
	sharedAuthClient = &http.Client{Transport: credentialTransport(func(r *http.Request) (*http.Response, error) {
		if r.Context().Err() != nil {
			t.Fatal("request context unexpectedly cancelled")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("refresh_token") != "refresh" {
			t.Fatal("refresh token not sent")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"new","expires_in":3600}`)), Header: make(http.Header)}, nil
	})}
	got, err := ValidOpenAICredential(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "new" || got.RefreshToken != "refresh" || got.AccountID != "account" {
		t.Fatal("refresh lost identity or token")
	}
	saved, err := GetCredential("openai")
	if err != nil || saved.AccessToken != "new" {
		t.Fatal("refresh not persisted")
	}
}

func TestValidOpenAICredentialSanitizesFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SetCredential("openai", &AuthCredential{AccessToken: "old", RefreshToken: "secret", AuthMethod: "oauth", ExpiresAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	original := sharedAuthClient
	t.Cleanup(func() { sharedAuthClient = original })
	sharedAuthClient = &http.Client{Transport: credentialTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("secret")), Header: make(http.Header)}, nil
	})}
	_, err := ValidOpenAICredential(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe diagnostic: %v", err)
	}
	saved, _ := GetCredential("openai")
	if saved.AccessToken != "old" {
		t.Fatal("failed refresh overwrote credential")
	}
}
