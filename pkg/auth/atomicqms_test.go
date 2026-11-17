package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAtomicQMS is a Gitea OAuth provider: it issues tokens for any code and
// answers /api/v1/user for issued tokens.
type fakeAtomicQMS struct {
	*httptest.Server
	exchanges atomic.Int32
	refreshes atomic.Int32
	issued    atomic.Int32
}

func newFakeAtomicQMS(t *testing.T) *fakeAtomicQMS {
	t.Helper()
	f := &fakeAtomicQMS{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			r.ParseForm()
			switch r.FormValue("grant_type") {
			case "authorization_code":
				if r.FormValue("code_verifier") == "" || r.FormValue("client_secret") != "" {
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
				f.exchanges.Add(1)
			case "refresh_token":
				f.refreshes.Add(1)
			default:
				http.Error(w, "bad grant", http.StatusBadRequest)
				return
			}
			n := f.issued.Add(1)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "access-" + string(rune('0'+n)),
				"refresh_token": "refresh-" + string(rune('0'+n)),
				"expires_in":    3600,
			})
		case "/api/v1/user":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer access-") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"login": "qmsuser"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func stateFromAuthorizeURL(t *testing.T, authURL string) string {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("state")
}

func TestAtomicQMSOAuthConfig(t *testing.T) {
	cfg := AtomicQMSOAuthConfig("https://qms.example.org/", "client-1")
	if cfg.authorizeEndpoint() != "https://qms.example.org/login/oauth/authorize" {
		t.Errorf("authorize endpoint = %q", cfg.authorizeEndpoint())
	}
	if cfg.tokenEndpoint() != "https://qms.example.org/login/oauth/access_token" {
		t.Errorf("token endpoint = %q", cfg.tokenEndpoint())
	}
	// Gitea grants full access when any scope is unknown, so the request must
	// be exactly these Gitea scopes.
	if cfg.Scopes != "read:repository read:issue read:user" {
		t.Errorf("scopes = %q", cfg.Scopes)
	}

	u, err := url.Parse(BuildAuthorizeURL(cfg, PKCECodes{CodeChallenge: "challenge"}, "state-1", "http://127.0.0.1:4142/atomicqms/callback"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Path != "/login/oauth/authorize" || q.Get("scope") != AtomicQMSScopes ||
		q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "client-1" {
		t.Errorf("authorize URL = %s", u)
	}
}

func TestOAuthProviderConfigDefaultEndpoints(t *testing.T) {
	cfg := OpenAIOAuthConfig()
	if cfg.authorizeEndpoint() != "https://auth.openai.com/authorize" || cfg.tokenEndpoint() != "https://auth.openai.com/oauth/token" {
		t.Errorf("OpenAI endpoints changed: %q %q", cfg.authorizeEndpoint(), cfg.tokenEndpoint())
	}
}

func TestAtomicQMSStates(t *testing.T) {
	const alice, bob = "discord:1001", "discord:2002"
	tests := []struct {
		name string
		// run begins whatever states it needs and returns the state and code to
		// complete with.
		run         func(t *testing.T, s *AtomicQMSStates, cfg OAuthProviderConfig, clock *time.Time) (string, string)
		wantErr     bool
		wantStored  []string
		wantMissing []string
	}{
		{
			name: "valid",
			run: func(t *testing.T, s *AtomicQMSStates, cfg OAuthProviderConfig, _ *time.Time) (string, string) {
				authURL, _ := s.Begin(cfg, "http://cb", alice)
				return stateFromAuthorizeURL(t, authURL), "code"
			},
			wantStored: []string{alice},
		},
		{
			name: "mismatched",
			run: func(t *testing.T, s *AtomicQMSStates, cfg OAuthProviderConfig, _ *time.Time) (string, string) {
				s.Begin(cfg, "http://cb", alice)
				return "not-the-issued-state", "code"
			},
			wantErr:     true,
			wantMissing: []string{alice},
		},
		{
			name: "expired",
			run: func(t *testing.T, s *AtomicQMSStates, cfg OAuthProviderConfig, clock *time.Time) (string, string) {
				authURL, _ := s.Begin(cfg, "http://cb", alice)
				*clock = clock.Add(atomicQMSStateTTL)
				return stateFromAuthorizeURL(t, authURL), "code"
			},
			wantErr:     true,
			wantMissing: []string{alice},
		},
		{
			name: "replayed",
			run: func(t *testing.T, s *AtomicQMSStates, cfg OAuthProviderConfig, _ *time.Time) (string, string) {
				authURL, _ := s.Begin(cfg, "http://cb", alice)
				state := stateFromAuthorizeURL(t, authURL)
				if _, err := s.Complete(state, "code"); err != nil {
					t.Fatalf("first completion: %v", err)
				}
				if err := DeleteCredential(AtomicQMSCredentialKey(alice)); err != nil {
					t.Fatal(err)
				}
				return state, "code"
			},
			wantErr:     true,
			wantMissing: []string{alice},
		},
		{
			name: "wrong sender",
			// Bob's newer connect does not touch Alice's state, and Alice's
			// state can only ever store Alice's credential.
			run: func(t *testing.T, s *AtomicQMSStates, cfg OAuthProviderConfig, _ *time.Time) (string, string) {
				authURL, _ := s.Begin(cfg, "http://cb", alice)
				s.Begin(cfg, "http://cb", bob)
				return stateFromAuthorizeURL(t, authURL), "code"
			},
			wantStored:  []string{alice},
			wantMissing: []string{bob},
		},
		{
			name: "superseded by same sender",
			run: func(t *testing.T, s *AtomicQMSStates, cfg OAuthProviderConfig, _ *time.Time) (string, string) {
				first, _ := s.Begin(cfg, "http://cb", alice)
				s.Begin(cfg, "http://cb", alice)
				return stateFromAuthorizeURL(t, first), "code"
			},
			wantErr:     true,
			wantMissing: []string{alice},
		},
		{
			name: "denied consent",
			run: func(t *testing.T, s *AtomicQMSStates, cfg OAuthProviderConfig, _ *time.Time) (string, string) {
				authURL, _ := s.Begin(cfg, "http://cb", alice)
				return stateFromAuthorizeURL(t, authURL), ""
			},
			wantErr:     true,
			wantMissing: []string{alice},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			provider := newFakeAtomicQMS(t)
			cfg := AtomicQMSOAuthConfig(provider.URL, "client-1")
			clock := time.Unix(1_800_000_000, 0)
			s := NewAtomicQMSStates()
			s.now = func() time.Time { return clock }

			state, code := tt.run(t, s, cfg, &clock)
			sender, err := s.Complete(state, code)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Complete() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && sender != tt.wantStored[0] {
				t.Errorf("Complete() sender = %q, want %q", sender, tt.wantStored[0])
			}
			for _, key := range tt.wantStored {
				cred, _ := GetCredential(AtomicQMSCredentialKey(key))
				if cred == nil || cred.AccessToken == "" || cred.AccountID != "qmsuser" || cred.Provider != "atomicqms" {
					t.Errorf("credential for %s = %+v", key, cred)
				}
			}
			for _, key := range tt.wantMissing {
				if cred, _ := GetCredential(AtomicQMSCredentialKey(key)); cred != nil {
					t.Errorf("unexpected credential for %s", key)
				}
			}
		})
	}
}

func TestAtomicQMSAccessToken(t *testing.T) {
	const sender = "telegram:42"
	tests := []struct {
		name          string
		stored        *AuthCredential
		rejected      string
		wantErr       error
		wantToken     string
		wantRefreshes int32
	}{
		{name: "not connected", wantErr: ErrAtomicQMSNotConnected},
		{
			name:      "fresh token",
			stored:    &AuthCredential{AccessToken: "stored", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)},
			wantToken: "stored",
		},
		{
			name:          "needs refresh",
			stored:        &AuthCredential{AccessToken: "stored", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Minute)},
			wantToken:     "access-1",
			wantRefreshes: 1,
		},
		{
			name:          "stored token rejected",
			stored:        &AuthCredential{AccessToken: "stored", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)},
			rejected:      "stored",
			wantToken:     "access-1",
			wantRefreshes: 1,
		},
		{
			name:      "already refreshed by another call",
			stored:    &AuthCredential{AccessToken: "newer", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)},
			rejected:  "older",
			wantToken: "newer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			provider := newFakeAtomicQMS(t)
			cfg := AtomicQMSOAuthConfig(provider.URL, "client-1")
			if tt.stored != nil {
				if err := SetCredential(AtomicQMSCredentialKey(sender), tt.stored); err != nil {
					t.Fatal(err)
				}
			}
			got, err := AtomicQMSAccessToken(cfg, sender, tt.rejected)
			if err != tt.wantErr {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.wantToken {
				t.Errorf("token = %q, want %q", got, tt.wantToken)
			}
			if provider.refreshes.Load() != tt.wantRefreshes {
				t.Errorf("refreshes = %d, want %d", provider.refreshes.Load(), tt.wantRefreshes)
			}
			if tt.wantRefreshes > 0 {
				cred, _ := GetCredential(AtomicQMSCredentialKey(sender))
				if cred.AccessToken != tt.wantToken || cred.RefreshToken == "" || cred.Provider != "atomicqms" {
					t.Errorf("stored after refresh = %+v", cred)
				}
			}
		})
	}
}
