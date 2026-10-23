// Package transport provides shared HTTP transports that use uTLS to present
// a Chrome-like TLS fingerprint. Go's default crypto/tls produces a JA3
// fingerprint that Cloudflare identifies as "Go" and blocks with a managed JS
// challenge. This package centralises the fix so all HTTP clients in the
// codebase (auth, LLM providers, etc.) can share it.
//
// NewTransport uses a Chrome 120 fingerprint with ALPN restricted to HTTP/1.1.
// NewCloudflareClient additionally adapts SDK headers and request compression.
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

	tlsConn := utls.UClient(rawConn, &utls.Config{
		ServerName: host,
		MinVersion: utls.VersionTLS12,
	}, utls.HelloCustom)
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
