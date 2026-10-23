package transport

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
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
	for name, client := range map[string]*http.Client{"http1": NewClient(), "chrome": NewH2Client()} {
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

func TestChromeTransportHTTP2(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("expected HTTP/2, got %s", r.Proto)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if r.Header.Get("X-Test") != "preserved" {
			t.Error("request header lost")
		}
		w.Header().Set("X-Response", "preserved")
		w.Write(body)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	// Only the loopback test server uses a self-signed certificate.
	inner, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithClientProfile(profiles.Chrome_120),
		tlsclient.WithNotFollowRedirects(), tlsclient.WithInsecureSkipVerify())
	if err != nil {
		t.Fatal(err)
	}
	defer inner.CloseIdleConnections()
	client := &http.Client{Transport: &chromeRoundTripper{client: inner}, Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("request-body"))
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
	if response.ProtoMajor != 2 || string(body) != "request-body" || response.Header.Get("X-Response") != "preserved" {
		t.Fatalf("unexpected HTTP/2 response: proto=%s body=%q headers=%v", response.Proto, body, response.Header)
	}
}
