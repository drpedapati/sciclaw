package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

// dialTLSForAuth performs a TLS handshake using uTLS with a Chrome-like
// ClientHello fingerprint. Go's default crypto/tls produces a JA3 fingerprint
// that Cloudflare identifies as "Go" and blocks with a managed JS challenge.
// By mimicking Chrome's TLS fingerprint, auth requests to Cloudflare-fronted
// endpoints (auth.openai.com) pass bot detection cleanly.
//
// HelloChrome_62 is used because:
//   - It presents a Chrome (not Go) JA3 fingerprint to Cloudflare
//   - It negotiates TLS 1.2, which passes through NAT/virtualisation stacks
//     (Multipass, Hyper-V) where TLS 1.3 handshakes are corrupted/rejected
//   - ALPN is restricted to HTTP/1.1 for net/http compatibility
func dialTLSForAuth(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	rawConn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		rawConn.Close()
		return nil, err
	}

	// Chrome 62 TLS 1.2 fingerprint — avoids both Cloudflare bot detection
	// (Go's default JA3) and NAT stack TLS 1.3 corruption.
	spec, err := utls.UTLSIdToSpec(utls.HelloChrome_62)
	if err != nil {
		rawConn.Close()
		return nil, err
	}
	for _, ext := range spec.Extensions {
		if alpn, ok := ext.(*utls.ALPNExtension); ok {
			alpn.AlpnProtocols = []string{"http/1.1"}
			break
		}
	}

	tlsConn := utls.UClient(rawConn, &utls.Config{ServerName: host}, utls.HelloCustom)
	if err := tlsConn.ApplyPreset(&spec); err != nil {
		rawConn.Close()
		return nil, err
	}
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		rawConn.Close()
		return nil, err
	}

	return tlsConn, nil
}

// sharedAuthClient is a single HTTP client reused across the entire auth flow.
// Sharing the client keeps the underlying TCP/TLS connection alive so Cloudflare
// doesn't rate-limit or challenge every poll as a brand-new connection.
// TLS is handled by dialTLSForAuth which uses uTLS with a Chrome fingerprint.
var sharedAuthClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		ForceAttemptHTTP2:  false,
		MaxIdleConns:       2,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: true,
		DialTLSContext:     dialTLSForAuth,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	},
}

// newAuthRequest creates an HTTP request with headers that match what the
// OpenAI Codex CLI sends (consistent with other OpenAI OAuth clients).
func newAuthRequest(method, url, contentType string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	return req, nil
}

// authPost performs an HTTP POST with retries for transient network errors.
func authPost(url, contentType string, body string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * 2 * time.Second
			fmt.Printf("  Retrying in %s (attempt %d/3)...\n", wait, attempt+1)
			time.Sleep(wait)
		}
		req, err := newAuthRequest("POST", url, contentType, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		resp, err := sharedAuthClient.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// authPostForm performs an HTTP POST with form encoding and retries.
func authPostForm(reqURL string, data url.Values) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * 2 * time.Second
			fmt.Printf("  Retrying in %s (attempt %d/3)...\n", wait, attempt+1)
			time.Sleep(wait)
		}
		req, err := newAuthRequest("POST", reqURL, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
		if err != nil {
			return nil, err
		}
		resp, err := sharedAuthClient.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

type OAuthProviderConfig struct {
	Issuer   string
	ClientID string
	Scopes   string
	Port     int
}

func OpenAIOAuthConfig() OAuthProviderConfig {
	return OAuthProviderConfig{
		Issuer:   "https://auth.openai.com",
		ClientID: "app_EMoamEEZ73f0CkXaXp7hrann",
		Scopes:   "openid profile email offline_access",
		Port:     1455,
	}
}

func generateState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func LoginBrowser(cfg OAuthProviderConfig) (*AuthCredential, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("generating PKCE: %w", err)
	}

	state, err := generateState()
	if err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}

	redirectURI := fmt.Sprintf("http://localhost:%d/auth/callback", cfg.Port)

	authURL := buildAuthorizeURL(cfg, pkce, state, redirectURI)

	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			resultCh <- callbackResult{err: fmt.Errorf("state mismatch")}
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			resultCh <- callbackResult{err: fmt.Errorf("no code received: %s", errMsg)}
			http.Error(w, "No authorization code received", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><h2>Authentication successful!</h2><p>You can close this window.</p></body></html>")
		resultCh <- callbackResult{code: code}
	})

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("starting callback server on port %d: %w", cfg.Port, err)
	}

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	fmt.Printf("Open this URL to authenticate:\n\n%s\n\n", authURL)

	if err := openBrowser(authURL); err != nil {
		fmt.Printf("Could not open browser automatically.\nPlease open this URL manually:\n\n%s\n\n", authURL)
	}

	fmt.Println("If you're running in a headless environment, use: picoclaw auth login --provider openai --device-code")
	fmt.Println("Waiting for authentication in browser...")

	select {
	case result := <-resultCh:
		if result.err != nil {
			return nil, result.err
		}
		return exchangeCodeForTokens(cfg, result.code, pkce.CodeVerifier, redirectURI)
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authentication timed out after 5 minutes")
	}
}

type callbackResult struct {
	code string
	err  error
}

type deviceCodeResponse struct {
	DeviceAuthID string
	UserCode     string
	Interval     int
}

func parseDeviceCodeResponse(body []byte) (deviceCodeResponse, error) {
	var raw struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		Interval     json.RawMessage `json:"interval"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return deviceCodeResponse{}, err
	}

	interval, err := parseFlexibleInt(raw.Interval)
	if err != nil {
		return deviceCodeResponse{}, err
	}

	return deviceCodeResponse{
		DeviceAuthID: raw.DeviceAuthID,
		UserCode:     raw.UserCode,
		Interval:     interval,
	}, nil
}

func parseFlexibleInt(raw json.RawMessage) (int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}

	var interval int
	if err := json.Unmarshal(raw, &interval); err == nil {
		return interval, nil
	}

	var intervalStr string
	if err := json.Unmarshal(raw, &intervalStr); err == nil {
		intervalStr = strings.TrimSpace(intervalStr)
		if intervalStr == "" {
			return 0, nil
		}
		return strconv.Atoi(intervalStr)
	}

	return 0, fmt.Errorf("invalid integer value: %s", string(raw))
}

func LoginDeviceCode(cfg OAuthProviderConfig) (*AuthCredential, error) {
	reqBody, _ := json.Marshal(map[string]string{
		"client_id": cfg.ClientID,
	})

	resp, err := authPost(
		cfg.Issuer+"/api/accounts/deviceauth/usercode",
		"application/json",
		string(reqBody),
	)
	if err != nil {
		return nil, fmt.Errorf("requesting device code: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code request failed: %s", string(body))
	}

	deviceResp, err := parseDeviceCodeResponse(body)
	if err != nil {
		return nil, fmt.Errorf("parsing device code response: %w", err)
	}

	if deviceResp.Interval < 1 {
		deviceResp.Interval = 5
	}

	fmt.Printf("\nTo authenticate, open this URL in your browser:\n\n  %s/codex/device\n\nThen enter this code: %s\n\nWaiting for authentication...\n",
		cfg.Issuer, deviceResp.UserCode)

	deadline := time.After(15 * time.Minute)
	pollInterval := time.Duration(deviceResp.Interval) * time.Second
	if pollInterval < 5*time.Second {
		pollInterval = 5 * time.Second
	}

	for {
		select {
		case <-deadline:
			return nil, fmt.Errorf("device code authentication timed out after 15 minutes")
		case <-time.After(pollInterval):
			cred, err := pollDeviceCode(cfg, deviceResp.DeviceAuthID, deviceResp.UserCode)
			if err != nil {
				if strings.Contains(err.Error(), "rate-limited") {
					pollInterval = pollInterval * 2
					if pollInterval > 30*time.Second {
						pollInterval = 30 * time.Second
					}
				}
				continue
			}
			if cred != nil {
				return cred, nil
			}
		}
	}
}

func pollDeviceCode(cfg OAuthProviderConfig, deviceAuthID, userCode string) (*AuthCredential, error) {
	reqBody, _ := json.Marshal(map[string]string{
		"device_auth_id": deviceAuthID,
		"user_code":      userCode,
	})

	resp, err := authPost(
		cfg.Issuer+"/api/accounts/deviceauth/token",
		"application/json",
		string(reqBody),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain body so the connection can be reused by the shared client
		io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("rate-limited (429), backing off")
		}
		return nil, fmt.Errorf("pending")
	}

	body, _ := io.ReadAll(resp.Body)

	var tokenResp struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeChallenge     string `json:"code_challenge"`
		CodeVerifier      string `json:"code_verifier"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	redirectURI := cfg.Issuer + "/deviceauth/callback"
	return exchangeCodeForTokens(cfg, tokenResp.AuthorizationCode, tokenResp.CodeVerifier, redirectURI)
}

func RefreshAccessToken(cred *AuthCredential, cfg OAuthProviderConfig) (*AuthCredential, error) {
	if cred.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	data := url.Values{
		"client_id":     {cfg.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {cred.RefreshToken},
		"scope":         {"openid profile email"},
	}

	resp, err := authPostForm(cfg.Issuer+"/oauth/token", data)
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed: %s", string(body))
	}

	return parseTokenResponse(body, cred.Provider)
}

func BuildAuthorizeURL(cfg OAuthProviderConfig, pkce PKCECodes, state, redirectURI string) string {
	return buildAuthorizeURL(cfg, pkce, state, redirectURI)
}

func buildAuthorizeURL(cfg OAuthProviderConfig, pkce PKCECodes, state, redirectURI string) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {cfg.ClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {cfg.Scopes},
		"code_challenge":        {pkce.CodeChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return cfg.Issuer + "/authorize?" + params.Encode()
}

func exchangeCodeForTokens(cfg OAuthProviderConfig, code, codeVerifier, redirectURI string) (*AuthCredential, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {cfg.ClientID},
		"code_verifier": {codeVerifier},
	}

	resp, err := authPostForm(cfg.Issuer+"/oauth/token", data)
	if err != nil {
		return nil, fmt.Errorf("exchanging code for tokens: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}

	return parseTokenResponse(body, "openai")
}

func parseTokenResponse(body []byte, provider string) (*AuthCredential, error) {
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		IDToken      string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("no access token in response")
	}

	var expiresAt time.Time
	if tokenResp.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	cred := &AuthCredential{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
		Provider:     provider,
		AuthMethod:   "oauth",
	}

	if accountID := extractAccountID(tokenResp.AccessToken); accountID != "" {
		cred.AccountID = accountID
	}

	return cred, nil
}

func extractAccountID(accessToken string) string {
	parts := strings.Split(accessToken, ".")
	if len(parts) < 2 {
		return ""
	}

	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64URLDecode(payload)
	if err != nil {
		return ""
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}

	if authClaim, ok := claims["https://api.openai.com/auth"].(map[string]interface{}); ok {
		if accountID, ok := authClaim["chatgpt_account_id"].(string); ok {
			return accountID
		}
	}

	return ""
}

func base64URLDecode(s string) ([]byte, error) {
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	return base64.StdEncoding.DecodeString(s)
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", url).Start()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
