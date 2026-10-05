package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ValidOpenAICredential refreshes the existing credential within the caller's
// deadline, without retry messages corrupting CLI discovery JSON.
func ValidOpenAICredential(ctx context.Context) (*AuthCredential, error) {
	cred, err := GetCredential("openai")
	if err != nil || cred == nil || cred.AccessToken == "" {
		return nil, fmt.Errorf("OpenAI login required")
	}
	if cred.AuthMethod != "oauth" || !cred.NeedsRefresh() {
		return cred, nil
	}
	if cred.RefreshToken == "" {
		if cred.IsExpired() {
			return nil, fmt.Errorf("OpenAI login expired")
		}
		return cred, nil
	}
	cfg := OpenAIOAuthConfig()
	data := url.Values{"client_id": {cfg.ClientID}, "grant_type": {"refresh_token"}, "refresh_token": {cred.RefreshToken}, "scope": {"openid profile email"}}
	req, err := newAuthRequest(http.MethodPost, cfg.Issuer+"/oauth/token", "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("OpenAI token refresh unavailable")
	}
	resp, err := sharedAuthClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("OpenAI token refresh unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenAI token refresh HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("invalid OpenAI token refresh response")
	}
	updated, err := parseTokenResponse(raw, "openai")
	if err != nil || updated.AccessToken == "" {
		return nil, fmt.Errorf("invalid OpenAI token refresh response")
	}
	if updated.RefreshToken == "" {
		updated.RefreshToken = cred.RefreshToken
	}
	if updated.AccountID == "" {
		updated.AccountID = cred.AccountID
	}
	if err := SetCredential("openai", updated); err != nil {
		return nil, fmt.Errorf("could not save OpenAI credential")
	}
	return updated, nil
}
