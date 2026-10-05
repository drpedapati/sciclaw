// Package codexidentity keeps catalog and inference client identity in sync.
package codexidentity

import "net/http"

const Originator = "codex_cli_rs"
const Version = "0.160.0"

func Headers(h http.Header) {
	h.Set("originator", Originator)
	h.Set("version", Version)
	h.Set("User-Agent", Originator+"/"+Version)
}
