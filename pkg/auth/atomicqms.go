package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AtomicQMSScopes is the exact scope string requested from AtomicQMS (Gitea).
// Gitea grants full API access when any requested scope is unknown
// (services/oauth2_provider/access_token.go GrantAdditionalScopes), so this
// must only name scopes from models/auth/access_token_scope.go.
const AtomicQMSScopes = "read:repository read:issue read:user"

const (
	atomicQMSProvider = "atomicqms"
	atomicQMSStateTTL = 10 * time.Minute
)

// ErrAtomicQMSNotConnected means the sender has no stored AtomicQMS credential.
var ErrAtomicQMSNotConnected = errors.New("atomicqms: not connected")

var errAtomicQMSState = errors.New("atomicqms: invalid or expired state")

// AtomicQMSOAuthConfig returns the public-client OAuth config for an AtomicQMS
// (Gitea) instance. No client secret is used; Gitea requires PKCE S256 for
// public clients.
func AtomicQMSOAuthConfig(baseURL, clientID string) OAuthProviderConfig {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return OAuthProviderConfig{
		Issuer:       base,
		ClientID:     strings.TrimSpace(clientID),
		Scopes:       AtomicQMSScopes,
		AuthorizeURL: base + "/login/oauth/authorize",
		TokenURL:     base + "/login/oauth/access_token",
	}
}

// AtomicQMSCredentialKey is the auth.json key for a sender. senderKey is the
// "<channel>:<sender_id>" identity the agent loop and per-user profiles use.
func AtomicQMSCredentialKey(senderKey string) string {
	return atomicQMSProvider + ":" + senderKey
}

// AtomicQMSStates holds pending authorization requests in memory. Each state
// is random, bound to one sender, single-use and short-lived.
type AtomicQMSStates struct {
	mu      sync.Mutex
	now     func() time.Time
	pending map[string]pendingAtomicQMS
}

type pendingAtomicQMS struct {
	senderKey   string
	cfg         OAuthProviderConfig
	verifier    string
	redirectURI string
	expiresAt   time.Time
}

func NewAtomicQMSStates() *AtomicQMSStates {
	return &AtomicQMSStates{now: time.Now, pending: map[string]pendingAtomicQMS{}}
}

// Begin records a pending authorization for senderKey and returns the URL the
// user opens to approve it. A newer Begin for the same sender replaces any
// earlier pending state for that sender.
func (s *AtomicQMSStates) Begin(cfg OAuthProviderConfig, redirectURI, senderKey string) (string, error) {
	if strings.TrimSpace(senderKey) == "" {
		return "", fmt.Errorf("atomicqms: sender is required")
	}
	pkce, err := GeneratePKCE()
	if err != nil {
		return "", err
	}
	state, err := generateState()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	now := s.now()
	for key, p := range s.pending {
		if p.senderKey == senderKey || !now.Before(p.expiresAt) {
			delete(s.pending, key)
		}
	}
	s.pending[state] = pendingAtomicQMS{
		senderKey:   senderKey,
		cfg:         cfg,
		verifier:    pkce.CodeVerifier,
		redirectURI: redirectURI,
		expiresAt:   now.Add(atomicQMSStateTTL),
	}
	s.mu.Unlock()

	return buildAuthorizeURL(cfg, pkce, state, redirectURI), nil
}

// take consumes a state. It fails closed for unknown, replayed or expired
// states.
func (s *AtomicQMSStates) take(state string) (pendingAtomicQMS, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[state]
	if !ok || state == "" {
		return pendingAtomicQMS{}, errAtomicQMSState
	}
	delete(s.pending, state)
	if !s.now().Before(p.expiresAt) {
		return pendingAtomicQMS{}, errAtomicQMSState
	}
	return p, nil
}

// Complete consumes state, exchanges code for tokens and stores them under the
// sender the state was issued to. It returns that sender's key.
func (s *AtomicQMSStates) Complete(state, code string) (string, error) {
	p, err := s.take(state)
	if err != nil {
		return "", err
	}
	if code == "" {
		return "", fmt.Errorf("atomicqms: no authorization code")
	}
	cred, err := exchangeCodeForTokens(p.cfg, code, p.verifier, p.redirectURI)
	if err != nil {
		return "", err
	}
	cred.Provider = atomicQMSProvider
	login, err := atomicQMSLogin(p.cfg, cred.AccessToken)
	if err != nil {
		return "", err
	}
	cred.AccountID = login
	if err := SetCredential(AtomicQMSCredentialKey(p.senderKey), cred); err != nil {
		return "", err
	}
	return p.senderKey, nil
}

// atomicQMSLogin returns the login of the user the token acts as.
func atomicQMSLogin(cfg OAuthProviderConfig, accessToken string) (string, error) {
	req, err := newAuthRequest(http.MethodGet, cfg.Issuer+"/api/v1/user", "application/json", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := sharedAuthClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("atomicqms: reading user: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("atomicqms: reading user: status %d", resp.StatusCode)
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(body, &user); err != nil || user.Login == "" {
		return "", fmt.Errorf("atomicqms: reading user: unexpected response")
	}
	return user.Login, nil
}

var atomicQMSRefreshLocks sync.Map // credential key -> *sync.Mutex

// AtomicQMSAccessToken returns a usable access token for senderKey. It
// refreshes the stored credential when it NeedsRefresh, or when rejected is
// the token currently stored (the server answered 401 for it). Refreshes are
// serialized per sender because Gitea rotates refresh tokens.
func AtomicQMSAccessToken(cfg OAuthProviderConfig, senderKey, rejected string) (string, error) {
	key := AtomicQMSCredentialKey(senderKey)
	lock, _ := atomicQMSRefreshLocks.LoadOrStore(key, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	cred, err := GetCredential(key)
	if err != nil {
		return "", err
	}
	if cred == nil || cred.AccessToken == "" {
		return "", ErrAtomicQMSNotConnected
	}
	if !cred.NeedsRefresh() && (rejected == "" || rejected != cred.AccessToken) {
		return cred.AccessToken, nil
	}
	fresh, err := RefreshAccessToken(cred, cfg)
	if err != nil {
		return "", err
	}
	fresh.Provider = atomicQMSProvider
	fresh.AccountID = cred.AccountID
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = cred.RefreshToken
	}
	if err := SetCredential(key, fresh); err != nil {
		return "", err
	}
	return fresh.AccessToken, nil
}
