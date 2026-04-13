package addons

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner is a scripted CommandRunner for signing tests. Each call to Run
// consumes one entry from calls; the entry's output and err are returned
// verbatim. Tests set calls slice up-front per scenario.
type fakeRunner struct {
	calls []fakeCall
	n     int
	// history is a log of (dir, script) that tests can assert against.
	history []fakeHistoryEntry
}

type fakeCall struct {
	// match, if non-empty, must appear in the script or the fake will
	// return a generic "unexpected script" error. Lets us keep tests
	// order-independent for branches that may or may not hit gpg.
	match  string
	output []byte
	err    error
}

type fakeHistoryEntry struct {
	dir    string
	script string
}

func (f *fakeRunner) Run(_ context.Context, dir, script string, _ []string) ([]byte, error) {
	f.history = append(f.history, fakeHistoryEntry{dir: dir, script: script})
	if f.n >= len(f.calls) {
		return nil, errors.New("fakeRunner: unexpected extra call: " + script)
	}
	c := f.calls[f.n]
	f.n++
	if c.match != "" && !strings.Contains(script, c.match) {
		return nil, errors.New("fakeRunner: script did not match expected substring; got: " + script)
	}
	return c.output, c.err
}

func TestVerifyTag_UnsignedTag(t *testing.T) {
	fr := &fakeRunner{calls: []fakeCall{{
		match:  "verify-tag",
		output: []byte("error: v0.1.0: cannot verify a non-tag object\nerror: no signature found\n"),
		err:    errors.New("exit 1"),
	}}}
	v := &Verifier{Runner: fr}
	res, err := v.VerifyTag(context.Background(), "/tmp/addon", "v0.1.0")
	if err != nil {
		t.Fatalf("VerifyTag: %v", err)
	}
	if res.Verified {
		t.Error("unsigned tag should not verify")
	}
	if res.Trusted {
		t.Error("unsigned tag should not be trusted")
	}
	if !strings.Contains(res.Warning, "unsigned") {
		t.Errorf("warning should mention unsigned, got %q", res.Warning)
	}
}

func TestVerifyTag_SignedTrusted(t *testing.T) {
	verifyOut := `gpg: Signature made Tue 14 Apr 2026 09:00:00 AM EDT
gpg:                using RSA key A1B2C3D4E5F6A7B8C9D0E1F2A3B4C5D6E7F80123
gpg: Good signature from "Ernie Pedapati <ernie@example.com>" [ultimate]
`
	gpgOut := `pub:u:4096:1:A1B2C3D4E5F60000:1700000000:::u:::scSC::::::0:
fpr:::::::::A1B2C3D4E5F6A7B8C9D0E1F2A3B4C5D6E7F80123:
uid:u::::1700000000::ABCDEF::Ernie Pedapati <ernie@example.com>::::::::::0:
`
	fr := &fakeRunner{calls: []fakeCall{
		{match: "verify-tag", output: []byte(verifyOut), err: nil},
		{match: "list-keys", output: []byte(gpgOut), err: nil},
	}}
	v := &Verifier{KeyringPath: "/keyring/trusted", Runner: fr}
	res, err := v.VerifyTag(context.Background(), "/tmp/addon", "v0.1.0")
	if err != nil {
		t.Fatalf("VerifyTag: %v", err)
	}
	if !res.Verified {
		t.Errorf("signed tag should verify; warning=%q", res.Warning)
	}
	if !res.Trusted {
		t.Errorf("key should be trusted; got untrusted warning=%q", res.Warning)
	}
	if res.SignerKeyID != "A1B2C3D4E5F6A7B8C9D0E1F2A3B4C5D6E7F80123" {
		t.Errorf("SignerKeyID = %q", res.SignerKeyID)
	}
	if !strings.Contains(res.SignerIdentity, "Ernie Pedapati") {
		t.Errorf("SignerIdentity = %q", res.SignerIdentity)
	}
	// Policy with trust anchor should admit the tag.
	if err := v.EnforceSignature(res, false); err != nil {
		t.Errorf("EnforceSignature: %v", err)
	}
}

func TestVerifyTag_SignedUntrusted(t *testing.T) {
	verifyOut := `gpg: Signature made Tue 14 Apr 2026 09:00:00 AM EDT
gpg:                using RSA key 1111222233334444555566667777888899990000
gpg: Good signature from "Stranger <stranger@example.com>" [unknown]
`
	// Keyring has a different key — A1B2... not 1111...
	gpgOut := `pub:u:4096:1:A1B2C3D4:::::::scSC::::::0:
fpr:::::::::A1B2C3D4E5F6A7B8C9D0E1F2A3B4C5D6E7F80123:
`
	fr := &fakeRunner{calls: []fakeCall{
		{match: "verify-tag", output: []byte(verifyOut), err: nil},
		{match: "list-keys", output: []byte(gpgOut), err: nil},
	}}
	v := &Verifier{KeyringPath: "/keyring/trusted", Runner: fr}
	res, err := v.VerifyTag(context.Background(), "/tmp/addon", "v0.1.0")
	if err != nil {
		t.Fatalf("VerifyTag: %v", err)
	}
	if !res.Verified {
		t.Error("signature itself is valid; Verified should be true")
	}
	if res.Trusted {
		t.Error("signer key is not in keyring; Trusted should be false")
	}
	if res.Warning == "" {
		t.Error("warning should be set for untrusted key")
	}
	// Strict policy should reject.
	if err := v.EnforceSignature(res, false); err == nil {
		t.Error("EnforceSignature should reject untrusted key")
	}
}

func TestVerifyTag_SignedMissingPublicKey(t *testing.T) {
	out := `gpg: Signature made Tue 14 Apr 2026 09:00:00 AM EDT
gpg:                using RSA key 1111222233334444
gpg: Can't check signature: No public key
`
	fr := &fakeRunner{calls: []fakeCall{{
		match:  "verify-tag",
		output: []byte(out),
		err:    errors.New("exit 1"),
	}}}
	v := &Verifier{Runner: fr}
	res, err := v.VerifyTag(context.Background(), "/tmp/addon", "v0.1.0")
	if err != nil {
		t.Fatalf("VerifyTag: %v", err)
	}
	if res.Verified {
		t.Error("missing public key should not verify")
	}
	if !strings.Contains(res.Warning, "not trusted") {
		t.Errorf("warning should mention untrusted, got %q", res.Warning)
	}
}

func TestEnforceSignature_AllowUntrustedOverride(t *testing.T) {
	v := &Verifier{KeyringPath: "/keyring/trusted"}
	res := &VerifyResult{Tag: "v0.1.0", Verified: false, Trusted: false, Warning: "unsigned"}
	if err := v.EnforceSignature(res, true); err != nil {
		t.Errorf("allowUntrusted=true should permit any result: %v", err)
	}
}

func TestEnforceSignature_EmptyKeyringPermitsUnsigned(t *testing.T) {
	v := &Verifier{}
	res := &VerifyResult{Tag: "v0.1.0", Verified: false, Warning: "tag unsigned"}
	if err := v.EnforceSignature(res, false); err != nil {
		t.Errorf("empty keyring should warn-but-allow unsigned: %v", err)
	}
}

func TestEnforceSignature_PopulatedKeyringRejectsUnsigned(t *testing.T) {
	v := &Verifier{KeyringPath: "/keyring/trusted"}
	res := &VerifyResult{Tag: "v0.1.0", Verified: false, Warning: "tag unsigned"}
	err := v.EnforceSignature(res, false)
	if err == nil {
		t.Fatal("populated keyring should reject unsigned tag")
	}
	if !strings.Contains(err.Error(), "unsigned") {
		t.Errorf("error should mention unsigned, got %v", err)
	}
}

func TestEnforceSignature_NilResultErrors(t *testing.T) {
	v := &Verifier{}
	if err := v.EnforceSignature(nil, false); err == nil {
		t.Error("nil result should error")
	}
}

func TestVerifyTag_EmptyTagErrors(t *testing.T) {
	v := &Verifier{Runner: &fakeRunner{}}
	if _, err := v.VerifyTag(context.Background(), "/tmp", ""); err == nil {
		t.Error("empty tag should error")
	}
}

func TestVerifyTag_NilVerifierErrors(t *testing.T) {
	var v *Verifier
	if _, err := v.VerifyTag(context.Background(), "/tmp", "v0.1.0"); err == nil {
		t.Error("nil verifier should error")
	}
}

func TestDefaultRunner_RunEchoesCommand(t *testing.T) {
	// Smoke-test DefaultRunner against a trivially safe /bin/sh builtin
	// so we exercise the production path. Skipped gracefully if /bin/sh
	// is not present (e.g., exotic CI).
	r := DefaultRunner{}
	out, err := r.Run(context.Background(), "", "printf hello", nil)
	if err != nil {
		t.Skipf("DefaultRunner unavailable: %v", err)
	}
	if string(out) != "hello" {
		t.Errorf("output = %q, want %q", string(out), "hello")
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"":              "''",
		"simple":        "'simple'",
		"with space":    "'with space'",
		"it's a thing":  `'it'\''s a thing'`,
		"$(evil)":       "'$(evil)'",
		"multi\nline":   "'multi\nline'",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLooksLikeFingerprint(t *testing.T) {
	cases := map[string]bool{
		"A1B2C3D4":                                 true,
		"A1B2C3D4E5F60000":                         true,
		"A1B2C3D4E5F6A7B8C9D0E1F2A3B4C5D6E7F80123": true,
		"NOTHEX00":                                 false,
		"short":                                    false,
		"":                                         false,
		"A1B2C3D":                                  false, // length 7
	}
	for in, want := range cases {
		if got := looksLikeFingerprint(in); got != want {
			t.Errorf("looksLikeFingerprint(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestVerifyTag_UnknownFailureSurfacesOutput(t *testing.T) {
	fr := &fakeRunner{calls: []fakeCall{{
		match:  "verify-tag",
		output: []byte("something unexpected and weird"),
		err:    errors.New("exit 2"),
	}}}
	v := &Verifier{Runner: fr}
	res, err := v.VerifyTag(context.Background(), "/tmp", "v0.1.0")
	if err != nil {
		t.Fatalf("VerifyTag: %v", err)
	}
	if res.Verified {
		t.Error("unknown failure should not verify")
	}
	if !strings.Contains(res.Warning, "verification failed") {
		t.Errorf("warning should describe failure, got %q", res.Warning)
	}
}
