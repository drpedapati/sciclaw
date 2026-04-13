package addons

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner runs an external command in a directory and returns combined
// output. Tests inject a fake runner so signature verification can be
// exercised without a real gpg/git installation; production code uses
// DefaultRunner.
//
// This interface shape is intentionally identical to the one Wave 2a defines
// in lifecycle.go so that both definitions are interchangeable at merge time.
// Signature:
//
//	Run(ctx context.Context, dir, script string, env []string) ([]byte, error)
type CommandRunner interface {
	Run(ctx context.Context, dir, script string, env []string) ([]byte, error)
}

// DefaultRunner is a CommandRunner that shells out to /bin/sh -c for the
// given script. It is used when Verifier.Runner is nil.
type DefaultRunner struct{}

// Run executes script via /bin/sh -c in dir with the supplied environment.
// The combined stdout+stderr is returned so callers can parse diagnostic
// output (e.g., gpg fingerprints from verify-tag stderr).
func (DefaultRunner) Run(ctx context.Context, dir, script string, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = env
	}
	return cmd.CombinedOutput()
}

// Verifier verifies GPG signatures on git tags. It is a thin layer over the
// `git verify-tag` and `gpg --list-keys` binaries; all external process
// execution flows through an injectable CommandRunner so tests can stub the
// command output without requiring gpg on the test host.
type Verifier struct {
	// KeyringPath is the directory containing trusted public keys
	// (e.g., ~/sciclaw/trusted-keys/). An empty keyring means "warn but
	// allow" for unsigned or untrusted tags; EnforceSignature relaxes
	// its policy in that case.
	KeyringPath string

	// Runner is the command runner used to invoke git and gpg.
	// Tests inject a fake; if nil, DefaultRunner is used.
	Runner CommandRunner
}

// VerifyResult describes the outcome of a single tag signature check. It is
// returned from VerifyTag in both the success and "signature missing or
// untrusted" cases — the caller then applies policy via EnforceSignature.
type VerifyResult struct {
	// Tag is the git tag that was checked.
	Tag string
	// SignerKeyID is the fingerprint of the signing key, if parseable
	// from git/gpg output. Empty when the tag was unsigned or the
	// signature could not be parsed.
	SignerKeyID string
	// SignerIdentity is the human-readable signer name, e.g.,
	// "Ernie Pedapati <ernie@example.com>". Empty when unavailable.
	SignerIdentity string
	// Trusted reports whether the signer key is present in the
	// configured keyring. Always false when KeyringPath is empty.
	Trusted bool
	// Verified reports whether `git verify-tag` considered the
	// signature cryptographically valid.
	Verified bool
	// Warning is a non-fatal explanatory message: "tag is unsigned",
	// "signer key not in keyring", etc. Empty on a clean, trusted verify.
	Warning string
}

// VerifyTag verifies the GPG signature of a git tag in the given addon
// directory.
//
// The return value is a *VerifyResult describing what was checked. It is
// non-nil for all non-infrastructure failures: an unsigned tag, a tag signed
// by an unknown key, or a cryptographically invalid signature all yield a
// populated result with Verified/Trusted reflecting the outcome and Warning
// set to an actionable message. An error is returned only when command
// infrastructure itself fails (e.g., /bin/sh missing, context cancelled).
//
// Policy decisions live in EnforceSignature.
func (v *Verifier) VerifyTag(ctx context.Context, addonDir, tag string) (*VerifyResult, error) {
	if v == nil {
		return nil, fmt.Errorf("signing.Verifier is nil")
	}
	if strings.TrimSpace(tag) == "" {
		return nil, fmt.Errorf("VerifyTag: tag must be non-empty")
	}
	runner := v.runner()

	// git verify-tag writes its diagnostic output (fingerprint, signer,
	// "no signature" message) to stderr. We use combined output so a
	// single parse handles every variant.
	script := fmt.Sprintf("git -C %s verify-tag %s", shellQuote(addonDir), shellQuote(tag))
	out, runErr := runner.Run(ctx, addonDir, script, nil)
	res := &VerifyResult{Tag: tag}
	parseVerifyTagOutput(string(out), res)

	if runErr == nil {
		res.Verified = true
	} else {
		// Non-zero exit is expected for unsigned or untrusted tags.
		// Distinguish the cases by scanning the output.
		res.Verified = false
		lower := strings.ToLower(string(out))
		switch {
		case strings.Contains(lower, "no signature") ||
			strings.Contains(lower, "not a signed tag") ||
			strings.Contains(lower, "does not have a gpg signature") ||
			strings.Contains(lower, "error: no signature found"):
			res.Warning = fmt.Sprintf("tag %q is unsigned; pass --allow-untrusted to install anyway", tag)
		case strings.Contains(lower, "no public key") ||
			strings.Contains(lower, "can't check signature") ||
			strings.Contains(lower, "public key not found"):
			res.Warning = fmt.Sprintf("tag %q is signed but the signing key is not trusted; import the signer's key into the keyring or pass --allow-untrusted", tag)
		default:
			// Treat "unknown" verify-tag failure modes as unverified
			// rather than as an infrastructure error so callers can
			// still apply the --allow-untrusted escape hatch. The
			// full output is surfaced through Warning.
			trimmed := strings.TrimSpace(string(out))
			if trimmed == "" {
				trimmed = runErr.Error()
			}
			res.Warning = fmt.Sprintf("tag %q signature verification failed: %s", tag, trimmed)
		}
	}

	// If a keyring is configured, consult gpg to decide whether the
	// fingerprint we observed belongs to a trusted key. When the tag was
	// unsigned there is nothing to check.
	if v.KeyringPath != "" && res.SignerKeyID != "" {
		trusted, trustErr := v.checkKeyringTrust(ctx, runner, res.SignerKeyID)
		if trustErr != nil {
			// Trust check failure is non-fatal; surface it as a warning
			// so operators can see it, but keep Verified intact.
			if res.Warning == "" {
				res.Warning = fmt.Sprintf("unable to consult keyring at %s: %v", v.KeyringPath, trustErr)
			}
		}
		res.Trusted = trusted
		if res.Verified && !trusted && res.Warning == "" {
			res.Warning = fmt.Sprintf("tag %q signer %s is not in keyring %s; pass --allow-untrusted to accept", tag, res.SignerKeyID, v.KeyringPath)
		}
	}

	return res, nil
}

// EnforceSignature returns an error if a VerifyResult does not meet policy.
//
// Policy:
//   - If allowUntrusted is true the result is accepted unconditionally.
//   - If the Verifier has a populated KeyringPath the result must be both
//     Verified and Trusted.
//   - If the Verifier's KeyringPath is empty, Verified=false is warnable
//     but not fatal — the caller is assumed to have no trust anchor and
//     the warning is surfaced via the returned result's Warning field.
func (v *Verifier) EnforceSignature(result *VerifyResult, allowUntrusted bool) error {
	if result == nil {
		return fmt.Errorf("EnforceSignature: result is nil")
	}
	if allowUntrusted {
		return nil
	}
	if v == nil || v.KeyringPath == "" {
		// No trust anchor configured. Unsigned/untrusted tags are
		// permitted; the warning on the result is the caller's signal.
		return nil
	}
	if !result.Verified {
		msg := result.Warning
		if msg == "" {
			msg = fmt.Sprintf("tag %q has no valid GPG signature", result.Tag)
		}
		return fmt.Errorf("signature policy: %s", msg)
	}
	if !result.Trusted {
		msg := result.Warning
		if msg == "" {
			msg = fmt.Sprintf("tag %q is signed by %s which is not in keyring %s", result.Tag, result.SignerKeyID, v.KeyringPath)
		}
		return fmt.Errorf("signature policy: %s", msg)
	}
	return nil
}

func (v *Verifier) runner() CommandRunner {
	if v.Runner != nil {
		return v.Runner
	}
	return DefaultRunner{}
}

// checkKeyringTrust shells out to `gpg --with-colons --list-keys` against the
// configured keyring to determine whether fingerprint is present. Comparison
// is case-insensitive and tolerates both long key IDs and full fingerprints.
func (v *Verifier) checkKeyringTrust(ctx context.Context, runner CommandRunner, fingerprint string) (bool, error) {
	script := fmt.Sprintf("gpg --with-colons --list-keys --keyring %s", shellQuote(v.KeyringPath))
	out, err := runner.Run(ctx, "", script, nil)
	if err != nil {
		return false, fmt.Errorf("gpg --list-keys: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	needle := strings.ToUpper(strings.TrimSpace(fingerprint))
	// --with-colons format: "fpr:::::::::<FINGERPRINT>:" or
	// "pub:u:4096:1:<KEYID>:..." — either form is a valid match target.
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.ToUpper(strings.TrimSpace(line))
		if line == "" {
			continue
		}
		fields := strings.Split(line, ":")
		for _, f := range fields {
			if f == "" {
				continue
			}
			if f == needle || strings.HasSuffix(f, needle) || strings.HasSuffix(needle, f) {
				if len(f) >= 8 {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// parseVerifyTagOutput extracts the signer key fingerprint and identity from
// `git verify-tag` output. git delegates to gpg which prints lines such as:
//
//	gpg: Signature made Tue 14 Apr 2026 09:00:00 AM EDT
//	gpg:                using RSA key A1B2C3D4E5F6...
//	gpg: Good signature from "Ernie Pedapati <ernie@example.com>" [ultimate]
//	gpg: WARNING: This key is not certified with a trusted signature!
//
// The parser is forgiving: missing fields simply remain empty.
func parseVerifyTagOutput(out string, res *VerifyResult) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.Contains(lower, "using") && strings.Contains(lower, "key"):
			// "gpg:                using RSA key A1B2C3D4..."
			if idx := strings.LastIndex(line, " "); idx != -1 && idx < len(line)-1 {
				candidate := strings.TrimSpace(line[idx+1:])
				if looksLikeFingerprint(candidate) {
					res.SignerKeyID = candidate
				}
			}
		case strings.Contains(lower, "good signature from"),
			strings.Contains(lower, "bad signature from"):
			// Extract quoted identity.
			if start := strings.Index(line, `"`); start != -1 {
				if end := strings.LastIndex(line, `"`); end > start {
					res.SignerIdentity = line[start+1 : end]
				}
			}
		}
	}
}

// looksLikeFingerprint returns true for strings that match the GPG
// fingerprint shape (hex of length 8, 16, or 40).
func looksLikeFingerprint(s string) bool {
	if len(s) != 8 && len(s) != 16 && len(s) != 40 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// shellQuote wraps s in single quotes, escaping any embedded single quote,
// so it can be safely pasted into a /bin/sh -c script.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
