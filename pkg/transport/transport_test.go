package transport

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func TestClientsFollowRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
			return
		}
		if r.Header.Get("X-Test") != "preserved" {
			t.Error("request header lost")
		}
		w.Header().Set("X-Response", "preserved")
		io.WriteString(w, "transport-ok")
	}))
	defer server.Close()
	for name, client := range map[string]*http.Client{"http1": NewClient(), "cloudflare": NewCloudflareClient()} {
		t.Run(name, func(t *testing.T) {
			client.Timeout = 5 * time.Second
			defer client.CloseIdleConnections()
			req, err := http.NewRequest(http.MethodGet, server.URL+"/redirect", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Test", "preserved")
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK || string(body) != "transport-ok" || response.Header.Get("X-Response") != "preserved" {
				t.Fatalf("unexpected response: status=%d body=%q headers=%v", response.StatusCode, body, response.Header)
			}
		})
	}
}

func TestCloudflareClientBodyAndHeaders(t *testing.T) {
	for _, size := range []int{2048, 2049} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			payload := strings.Repeat("a", size)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Stainless-Lang") != "" {
					t.Error("SDK telemetry header retained")
				}
				if r.Header.Get("Authorization") != "Bearer test-only" {
					t.Error("authorization header lost")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				if size > 2048 {
					if r.Header.Get("Content-Encoding") != "zstd" {
						t.Error("large body was not compressed")
					}
					decoder, err := zstd.NewReader(nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer decoder.Close()
					body, err = decoder.DecodeAll(body, nil)
					if err != nil {
						t.Error(err)
						return
					}
				} else if r.Header.Get("Content-Encoding") != "" {
					t.Error("small body unexpectedly compressed")
				}
				if string(body) != payload {
					t.Error("request body changed")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			client := NewCloudflareClient()
			client.Timeout = 5 * time.Second
			defer client.CloseIdleConnections()
			req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Stainless-Lang", "go")
			req.Header.Set("Authorization", "Bearer test-only")
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				t.Fatalf("status=%d", response.StatusCode)
			}
		})
	}
}

// Run each certificate scenario in a subprocess because crypto/x509 caches the
// system certificate pool. A fallback root pool gives each child a portable
// test trust store while leaving production certificate verification enabled.
func TestClientsTLS(t *testing.T) {
	for _, version := range []uint16{tls.VersionTLS12, tls.VersionTLS13} {
		t.Run(fmt.Sprintf("TLS%x", version), func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.TLS == nil || r.TLS.Version != version || r.ProtoMajor != 1 {
					t.Errorf("unexpected TLS/protocol: TLS=%v HTTP=%s", r.TLS, r.Proto)
				}
				io.WriteString(w, "tls-ok")
			}))
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			server.TLS = &tls.Config{MinVersion: version, MaxVersion: version}
			server.StartTLS()
			defer server.Close()
			roots := t.TempDir()
			certFile := filepath.Join(roots, "roots.pem")
			cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			if err := os.WriteFile(certFile, cert, 0600); err != nil {
				t.Fatal(err)
			}
			for _, trusted := range []bool{true, false} {
				cmd := exec.Command(os.Args[0], "-test.run=^TestClientsTLSProbe$", "-test.v")
				debug := os.Getenv("GODEBUG")
				if debug != "" {
					debug += ","
				}
				cmd.Env = append(os.Environ(), "SCICLAW_TEST_TLS_URL="+server.URL,
					fmt.Sprintf("SCICLAW_TEST_TLS_TRUSTED=%t", trusted), "SCICLAW_TEST_TLS_CA="+certFile,
					"GODEBUG="+debug+"x509usefallbackroots=1")
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("trusted=%t: %v\n%s", trusted, err, out)
				}
			}
		})
	}
}

func TestClientsTLSProbe(t *testing.T) {
	url := os.Getenv("SCICLAW_TEST_TLS_URL")
	if url == "" {
		t.Skip("subprocess helper")
	}
	trusted := os.Getenv("SCICLAW_TEST_TLS_TRUSTED") == "true"
	roots := x509.NewCertPool()
	if trusted {
		cert, err := os.ReadFile(os.Getenv("SCICLAW_TEST_TLS_CA"))
		if err != nil {
			t.Fatal(err)
		}
		if !roots.AppendCertsFromPEM(cert) {
			t.Fatal("test root is not valid PEM")
		}
	}
	x509.SetFallbackRoots(roots)

	for name, client := range map[string]*http.Client{"http1": NewClient(), "cloudflare": NewCloudflareClient()} {
		t.Run(name, func(t *testing.T) {
			client.Timeout = 5 * time.Second
			defer client.CloseIdleConnections()
			response, err := client.Get(url)
			if !trusted {
				if response != nil {
					response.Body.Close()
				}
				var unknown x509.UnknownAuthorityError
				if !errors.As(err, &unknown) {
					t.Fatalf("expected unknown-authority rejection, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK || string(body) != "tls-ok" || response.ProtoMajor != 1 {
				t.Fatalf("unexpected response: status=%d body=%q proto=%s", response.StatusCode, body, response.Proto)
			}
		})
	}
}
