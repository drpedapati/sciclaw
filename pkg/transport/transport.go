// Package transport provides shared HTTP transports that use uTLS to present
// a Chrome-like TLS fingerprint. Go's default crypto/tls produces a JA3
// fingerprint that Cloudflare identifies as "Go" and blocks with a managed JS
// challenge. This package centralises the fix so all HTTP clients in the
// codebase (auth, LLM providers, etc.) can share it.
//
// Two transport variants are provided:
//   - NewTransport (HTTP/1.1) — for auth endpoints and API providers that
//     accept HTTP/1.1. Uses Chrome 120 fingerprint with ALPN restricted to
//     http/1.1 to force HTTP/1.1 negotiation.
//   - NewH2Client (HTTP/2) — for endpoints like chatgpt.com that require
//     HTTP/2. Uses tls-client with Chrome 120 profile which provides matching
//     TLS + HTTP/2 SETTINGS fingerprints (header table size, initial window
//     size, pseudo-header order, connection flow).
package transport

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	utls "github.com/refraction-networking/utls"
)

// dialChromeTLSh1 is like dialChromeTLS but restricts ALPN to HTTP/1.1 only.
// This prevents the server from negotiating h2, which Go's http.Transport
// cannot handle over custom DialTLSContext connections.
// Wrapped with h1Conn to hide ConnectionState from Go's h2 detection.
func dialChromeTLSh1(ctx context.Context, network, addr string) (net.Conn, error) {
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

	spec, err := utls.UTLSIdToSpec(utls.HelloChrome_120)
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
	if err := tlsConn.Handshake(); err != nil {
		rawConn.Close()
		return nil, err
	}

	// Wrap to prevent Go's net/http from detecting h2 on the connection.
	return &h1Conn{Conn: tlsConn}, nil
}

// h1Conn wraps a net.Conn to hide ConnectionState from Go's net/http Transport.
type h1Conn struct {
	net.Conn
}

// NewTransport returns an *http.Transport using Chrome TLS fingerprint with
// HTTP/1.1 only. Suitable for auth endpoints and most API providers.
func NewTransport() *http.Transport {
	return &http.Transport{
		ForceAttemptHTTP2:  false,
		MaxIdleConns:       4,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: true,
		DialTLSContext:     dialChromeTLSh1,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
}

// NewProxyTransport returns an HTTP/1.1 transport with Chrome TLS fingerprint
// and an HTTP proxy configured.
func NewProxyTransport(proxyURL *url.URL) *http.Transport {
	t := NewTransport()
	t.Proxy = http.ProxyURL(proxyURL)
	return t
}

// NewClient returns an *http.Client using the HTTP/1.1 Chrome-fingerprinted
// transport. Timeout of 0 means no timeout (suitable for streaming LLM calls).
func NewClient() *http.Client {
	return &http.Client{
		Timeout:   0,
		Transport: NewTransport(),
	}
}

// NewCloudflareClient returns an *http.Client for Cloudflare-fronted endpoints
// like chatgpt.com. It strips SDK telemetry headers (X-Stainless-*) and applies
// Zstd compression to request bodies >2KB to stay under Cloudflare's WAF limits.
// Uses HTTP/1.1 Chrome TLS fingerprint.
func NewCloudflareClient() *http.Client {
	return &http.Client{
		Timeout:   0,
		Transport: &cloudflareRT{inner: NewTransport()},
	}
}

// cloudflareRT adapts requests for Cloudflare-fronted endpoints:
//  1. Strips X-Stainless-* telemetry headers that trigger WAF rules.
//  2. Compresses request bodies >2KB with Zstd (matching the official Codex CLI).
type cloudflareRT struct {
	inner http.RoundTripper
}

func (rt *cloudflareRT) RoundTrip(req *http.Request) (*http.Response, error) {
	// Strip SDK telemetry headers.
	for k := range req.Header {
		if strings.HasPrefix(k, "X-Stainless") {
			req.Header.Del(k)
		}
	}

	// Compress body with Zstd if >2KB.
	if req.Body != nil && req.ContentLength > 2048 {
		raw, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
		if err != nil {
			return nil, err
		}
		compressed := enc.EncodeAll(raw, nil)
		enc.Close()
		req.Body = io.NopCloser(bytes.NewReader(compressed))
		req.ContentLength = int64(len(compressed))
		req.Header.Set("Content-Encoding", "zstd")
	}

	return rt.inner.RoundTrip(req)
}

// chromeRoundTripper adapts tls-client (which uses bogdanfinn/fhttp types)
// to Go's standard http.RoundTripper interface. It converts http.Request to
// fhttp.Request, delegates to the tls-client, then converts the response back.
type chromeRoundTripper struct {
	client tlsclient.HttpClient
}

func (rt *chromeRoundTripper) RoundTrip(hReq *http.Request) (*http.Response, error) {
	var body io.Reader
	if hReq.Body != nil {
		body = hReq.Body
	}
	fReq, err := fhttp.NewRequest(hReq.Method, hReq.URL.String(), body)
	if err != nil {
		return nil, err
	}
	// Copy headers individually so fhttp's internal defaults are preserved.
	// Replacing the whole map (fReq.Header = ...) breaks tls-client's
	// Cloudflare bypass on chatgpt.com (returns 403).
	for k, vv := range hReq.Header {
		for _, v := range vv {
			fReq.Header.Add(k, v)
		}
	}
	if hReq.ContentLength > 0 {
		fReq.ContentLength = hReq.ContentLength
	}

	fResp, err := rt.client.Do(fReq)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		Status:           fResp.Status,
		StatusCode:       fResp.StatusCode,
		Proto:            fResp.Proto,
		ProtoMajor:       fResp.ProtoMajor,
		ProtoMinor:       fResp.ProtoMinor,
		Header:           http.Header(fResp.Header),
		Body:             fResp.Body,
		ContentLength:    fResp.ContentLength,
		TransferEncoding: fResp.TransferEncoding,
		Close:            fResp.Close,
		Uncompressed:     fResp.Uncompressed,
		Trailer:          http.Header(fResp.Trailer),
		Request:          hReq,
	}, nil
}

// NewH2Client returns an *http.Client that speaks HTTP/2 using a full Chrome
// browser fingerprint. Required for Cloudflare-fronted endpoints like
// chatgpt.com that inspect both TLS ClientHello (JA3) and HTTP/2 SETTINGS
// frame fingerprints (Akamai h2 fingerprint). Uses tls-client with Chrome 120
// profile to match Chrome's TLS extensions, cipher suites, HTTP/2 SETTINGS
// values/order, pseudo-header order, and connection flow.
func NewH2Client() *http.Client {
	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithClientProfile(profiles.Chrome_120),
		tlsclient.WithRandomTLSExtensionOrder(),
		tlsclient.WithNotFollowRedirects(),
	)
	if err != nil {
		panic("transport: creating Chrome h2 client: " + err.Error())
	}
	return &http.Client{
		Timeout:   0,
		Transport: &chromeRoundTripper{client: client},
	}
}
