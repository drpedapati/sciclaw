package main

// addon_cmd.go wires the pkg/addons/ data plane primitives (Store, Lifecycle,
// Verifier, Rollbacker, Export, WriteSBOM) into the top-level `sciclaw addon`
// subcommand group.
//
// Style matches cmd/picoclaw/routing_cmd.go: hand-rolled arg parsing, no Cobra
// or flag stdlib, help text via Printf with invokedCLIName(). Errors go to
// stderr and non-zero exits come from os.Exit in the thin outer wrappers.
//
// Command logic is split into pure functions taking (ctx, env, args) so they
// can be unit-tested with a fake addonLifecycle. The top-level addonXxxCmd
// wrappers (invoked from main.go) just resolve the real lifecycle, wire
// stdin/stdout/stderr, and propagate the returned exit code via os.Exit.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/sipeed/picoclaw/pkg/addons"
)

// addonLifecycle is the subset of *addons.Lifecycle the CLI handlers use.
// Tests implement it with a fake (addon_cmd_test.go) so every flow except
// the thin wrappers that build a real Lifecycle is exercisable without git
// or a real filesystem.
type addonLifecycle interface {
	Install(ctx context.Context, opts addons.InstallOptions) (*addons.RegistryEntry, error)
	Enable(ctx context.Context, name string) (*addons.RegistryEntry, error)
	Disable(ctx context.Context, name string) (*addons.RegistryEntry, error)
	Uninstall(ctx context.Context, name string, force bool) error
	Upgrade(ctx context.Context, name string, ref addons.InstallRef) (*addons.RegistryEntry, error)
	List(ctx context.Context) ([]*addons.RegistryEntry, error)
	AddonDir(name string) string
}

// addonEnv carries every side-effecting dependency a CLI handler needs so
// tests can inject fakes. All fields are non-nil in production callers.
type addonEnv struct {
	Lifecycle addonLifecycle
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	// entryByName is an optional registry reader injected by tests.
	// Production callers leave it nil and fall back to the real Store
	// rooted at sciclawHomeDir(). Kept as a function (not a *Store) so
	// tests don't have to touch disk.
	entryByName func(name string) (*addons.RegistryEntry, error)
	// names returns all registered addon names in sorted order.
	names func() ([]string, error)
}

// newProdEnv returns an env wired up with the real Lifecycle and registry.
// Used only from the outer wrappers; tests build their own env.
func newProdEnv() *addonEnv {
	home := sciclawHomeDir()
	lc := buildLifecycle()
	store := addons.NewStore(home)
	return &addonEnv{
		Lifecycle:   lc,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		entryByName: store.Get,
		names:       store.List,
	}
}

// --- top-level dispatch -----------------------------------------------------

func addonCmd() {
	if len(os.Args) < 3 {
		addonHelp(os.Stdout)
		return
	}

	sub := strings.ToLower(strings.TrimSpace(os.Args[2]))
	args := os.Args[3:]
	ctx, stop := ctxWithSignals()
	defer stop()

	env := newProdEnv()

	var code int
	switch sub {
	case "list":
		code = runList(ctx, env)
	case "status":
		code = runStatus(ctx, env, args)
	case "install":
		code = runInstall(ctx, env, args)
	case "enable":
		code = runEnable(ctx, env, args)
	case "disable":
		code = runDisable(ctx, env, args)
	case "uninstall":
		code = runUninstall(ctx, env, args)
	case "upgrade":
		code = runUpgrade(ctx, env, args)
	case "verify":
		code = runVerify(ctx, env, args)
	case "rollback":
		code = runRollback(ctx, env, args)
	case "sbom":
		code = runSBOM(ctx, env, args)
	case "help", "-h", "--help":
		addonHelp(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "Unknown addon command: %s\n", sub)
		addonHelp(os.Stderr)
		code = 1
	}
	if code != 0 {
		os.Exit(code)
	}
}

func addonHelp(w io.Writer) {
	cn := invokedCLIName()
	fmt.Fprintf(w, "\nAddon commands — install and manage sciClaw addons (pkg/addons/ data plane).\n\n")
	fmt.Fprintf(w, "Usage: %s addon <subcommand> [flags]\n\n", cn)
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintf(w, "  %s addon list                          List installed addons\n", cn)
	fmt.Fprintf(w, "  %s addon status <name>                 Show detailed info + integrity check\n", cn)
	fmt.Fprintf(w, "  %s addon install <source> [flags]      Clone, pin, and register an addon\n", cn)
	fmt.Fprintf(w, "  %s addon enable <name>                 Verify integrity and mark enabled\n", cn)
	fmt.Fprintf(w, "  %s addon disable <name>                Mark addon installed-but-not-enabled\n", cn)
	fmt.Fprintf(w, "  %s addon uninstall <name> [--force]    Remove addon + registry entry\n", cn)
	fmt.Fprintf(w, "  %s addon upgrade <name> [flags]        Advance to a new commit / version / track\n", cn)
	fmt.Fprintf(w, "  %s addon verify <name>                 Re-run integrity checks against registry\n", cn)
	fmt.Fprintf(w, "  %s addon rollback <name> [--yes]       Revert to previously installed commit\n", cn)
	fmt.Fprintf(w, "  %s addon sbom <name> [--output path]   Export JSON SBOM for audit\n", cn)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Install / upgrade flags:")
	fmt.Fprintln(w, "  --commit <sha>         Pin to exact commit SHA")
	fmt.Fprintln(w, "  --version <ver>        Pin to a version tag (e.g. v0.1.0)")
	fmt.Fprintln(w, "  --track <branch>       Opt into branch tracking (not recommended for production)")
	fmt.Fprintln(w, "  --name <addon-name>    Override the expected addon.json name (install only)")
	fmt.Fprintln(w, "  --yes                  Skip confirmation prompt")
}

// --- build a real Lifecycle from paths / git --------------------------------

// sciclawHomeDir returns the sciClaw home directory (parent of config.json).
// TODO: when pkg/paths.AppHome() lands from the migration RFC, replace this.
func sciclawHomeDir() string {
	return filepath.Dir(getConfigPath())
}

// sciclawVersionForAddons returns the version string used for
// requires.sciclaw constraint checking. Dev builds fall back to a plausible
// release number so manifest checks don't hard-fail during development.
func sciclawVersionForAddons() string {
	v := strings.TrimPrefix(version, "v")
	if v == "" || v == "dev" {
		// In sync with docs/issues/addon-system-rfc.md "sciClaw 0.3.0"
		// deployment checklist. Real releases override via ldflags.
		return "0.3.0-dev"
	}
	return v
}

// validateCloneURL rejects URLs that begin with '-' (would be parsed as a
// git flag), that use a scheme outside the allowlist, or that otherwise
// look hostile. The allowlist is the set of transports real addons are
// expected to use: https, http (lab intranets), git (read-only), ssh,
// and the git-ssh shorthand "user@host:path".
func validateCloneURL(repoURL string) error {
	u := strings.TrimSpace(repoURL)
	if u == "" {
		return fmt.Errorf("git clone: repo URL is empty")
	}
	if strings.HasPrefix(u, "-") {
		return fmt.Errorf("git clone: repo URL %q must not start with '-'", u)
	}
	schemes := []string{"https://", "http://", "ssh://", "git://", "git+ssh://", "file://"}
	for _, s := range schemes {
		if strings.HasPrefix(u, s) {
			return nil
		}
	}
	// user@host:path shorthand, e.g. git@github.com:foo/bar.git
	if at := strings.IndexByte(u, '@'); at > 0 {
		if colon := strings.IndexByte(u[at:], ':'); colon > 0 {
			return nil
		}
	}
	return fmt.Errorf("git clone: repo URL %q must use https, http, ssh, git, or user@host:path", u)
}

// gitCloneFn shells out to `git clone`. Returns an error that wraps git's
// combined output for actionability. Context cancellation propagates to
// the child process.
func gitCloneFn(ctx context.Context, repoURL, dest string) error {
	if strings.TrimSpace(repoURL) == "" {
		return fmt.Errorf("git clone: repo URL is empty")
	}
	if strings.TrimSpace(dest) == "" {
		return fmt.Errorf("git clone: destination is empty")
	}
	if err := validateCloneURL(repoURL); err != nil {
		return err
	}
	// Lifecycle.Install pre-creates the staging dir; git refuses to clone
	// into a non-empty directory, so we clear it first.
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("git clone: clearing staging %s: %w", dest, err)
	}
	// `--` is mandatory here: without it, a repoURL starting with `-` (e.g.,
	// `--upload-pack=/tmp/evil`) would be parsed as a git option and turn
	// `addon install` into arbitrary command execution (CVE-2017-1000117
	// class).
	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", "--", repoURL, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed == "" {
			return fmt.Errorf("git clone %s: %w", repoURL, err)
		}
		return fmt.Errorf("git clone %s: %w (output: %s)", repoURL, err, trimmed)
	}
	return nil
}

func buildLifecycle() *addons.Lifecycle {
	home := sciclawHomeDir()
	store := addons.NewStore(home)
	lc := addons.New(store, home, sciclawVersionForAddons(), runtime.GOOS)
	lc.LookPath = exec.LookPath
	lc.Clone = gitCloneFn
	lc.Runner = addons.DefaultRunner{}
	lc.Now = time.Now
	// CLI does not own sidecars — the gateway reconciles via addons.Reconciler.
	//
	// The CLI runs in a short-lived process: any sidecar it spawned would die
	// on CLI exit. Instead, Enable/Disable/Upgrade/Uninstall here mutate the
	// registry.json state only, and the long-lived gateway's *Reconciler
	// notices the state change (either on the next tick or immediately via
	// the reload marker written by addons.TriggerReload below) and converges
	// its live *SidecarRegistry to match.
	lc.Registry = nil
	return lc
}

// triggerGatewayReload nudges any running gateway's Reconciler to perform a
// reconcile pass immediately instead of waiting for the next ticker tick.
// Failures are logged as warnings (not errors) because the eventual-
// consistency path still converges without the marker.
func triggerGatewayReload(w io.Writer) {
	if err := addons.TriggerReload(sciclawHomeDir()); err != nil {
		fmt.Fprintf(w, "warning: could not notify gateway to reload addons: %v\n", err)
	}
}

func ctxWithSignals() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
	}()
	stop := func() {
		signal.Stop(ch)
		cancel()
	}
	return ctx, stop
}

// --- shared flag parsing ----------------------------------------------------

// installFlags holds the shared --commit/--version/--track/--name/--yes set.
// Used by both `install` (all flags) and `upgrade` (ignores --name).
type installFlags struct {
	Commit  string
	Version string
	Track   string
	Name    string
	Yes     bool
}

// errShowHelp is returned when the user passed -h/--help so callers can
// print subcommand help and exit 0 without treating it as a parse error.
var errShowHelp = errors.New("show help")

// parseInstallFlags consumes the shared install/upgrade flag set. At most
// one of commit/version/track may be set; "auto-latest-signed-tag" is the
// default when all three are empty.
func parseInstallFlags(args []string) (installFlags, error) {
	out := installFlags{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--commit":
			if i+1 >= len(args) {
				return out, fmt.Errorf("--commit requires a value")
			}
			out.Commit = strings.TrimSpace(args[i+1])
			i++
		case "--version":
			if i+1 >= len(args) {
				return out, fmt.Errorf("--version requires a value")
			}
			out.Version = strings.TrimSpace(args[i+1])
			i++
		case "--track":
			if i+1 >= len(args) {
				return out, fmt.Errorf("--track requires a value")
			}
			out.Track = strings.TrimSpace(args[i+1])
			i++
		case "--name":
			if i+1 >= len(args) {
				return out, fmt.Errorf("--name requires a value")
			}
			out.Name = strings.TrimSpace(args[i+1])
			i++
		case "--yes", "-y":
			out.Yes = true
		case "-h", "--help":
			return out, errShowHelp
		default:
			return out, fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	set := 0
	if out.Commit != "" {
		set++
	}
	if out.Version != "" {
		set++
	}
	if out.Track != "" {
		set++
	}
	if set > 1 {
		return out, fmt.Errorf("pass at most one of --commit, --version, --track")
	}
	return out, nil
}

func (f installFlags) toInstallRef() addons.InstallRef {
	switch {
	case f.Commit != "":
		return addons.NewCommitRef(f.Commit)
	case f.Version != "":
		return addons.NewVersionRef(f.Version)
	case f.Track != "":
		return addons.NewTrackRef(f.Track)
	default:
		return addons.NewAutoRef()
	}
}

func (f installFlags) describeRef() string {
	switch {
	case f.Commit != "":
		return "commit " + f.Commit
	case f.Version != "":
		return "version " + f.Version
	case f.Track != "":
		return "branch " + f.Track + " (track mode — not recommended for production)"
	default:
		return "latest signed tag (auto)"
	}
}

// --- confirmation helpers ---------------------------------------------------

func promptYes(in io.Reader, out io.Writer, prompt string, assumeYes bool) bool {
	if assumeYes {
		return true
	}
	fmt.Fprintf(out, "%s [y/N] ", prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

func promptTypedName(in io.Reader, out io.Writer, name string) bool {
	fmt.Fprintf(out, "Type the addon name (%s) to confirm: ", name)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		return false
	}
	return strings.TrimSpace(line) == name
}

// --- list -------------------------------------------------------------------

func runList(ctx context.Context, env *addonEnv) int {
	entries, err := env.Lifecycle.List(ctx)
	if err != nil {
		fmt.Fprintf(env.Stderr, "Error listing addons: %v\n", err)
		return 1
	}

	// Lifecycle.List returns in registry order. We want name ordering, so
	// we re-derive names from the environment's name listing when possible.
	var names []string
	if env.names != nil {
		names, err = env.names()
		if err != nil {
			fmt.Fprintf(env.Stderr, "Error reading registry: %v\n", err)
			return 1
		}
	}

	byName := map[string]*addons.RegistryEntry{}
	if env.entryByName != nil {
		for _, n := range names {
			e, err := env.entryByName(n)
			if err != nil || e == nil {
				continue
			}
			byName[n] = e
		}
	}
	// Fallback: build from Lifecycle.List's output. We can't recover names
	// without the Store, so this path only fires when a test supplied a
	// Lifecycle but no names — in that case rows are anonymous.
	if len(byName) == 0 {
		for i, e := range entries {
			byName[fmt.Sprintf("entry-%d", i)] = e
		}
		names = make([]string, 0, len(byName))
		for n := range byName {
			names = append(names, n)
		}
		sort.Strings(names)
	}

	rows := make([]addonListRow, 0, len(names))
	for _, n := range names {
		e, ok := byName[n]
		if !ok {
			continue
		}
		rows = append(rows, rowFromEntry(n, e))
	}

	if len(rows) == 0 {
		fmt.Fprintln(env.Stdout, "no addons installed")
		return 0
	}
	fmt.Fprint(env.Stdout, formatAddonListTable(rows))
	return 0
}

type addonListRow struct {
	Name    string
	Version string
	State   string
	Commit  string
	Source  string
}

func rowFromEntry(name string, entry *addons.RegistryEntry) addonListRow {
	return addonListRow{
		Name:    name,
		Version: entry.Version,
		State:   string(entry.State),
		Commit:  shortCommit(entry.InstalledCommit),
		Source:  entry.Source,
	}
}

func shortCommit(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// formatAddonListTable renders rows as a fixed-width table with header.
func formatAddonListTable(rows []addonListRow) string {
	header := addonListRow{
		Name: "NAME", Version: "VERSION", State: "STATE", Commit: "COMMIT", Source: "SOURCE",
	}
	wName, wVer, wState, wCommit := len(header.Name), len(header.Version), len(header.State), len(header.Commit)
	for _, r := range rows {
		if n := len(r.Name); n > wName {
			wName = n
		}
		if n := len(r.Version); n > wVer {
			wVer = n
		}
		if n := len(r.State); n > wState {
			wState = n
		}
		if n := len(r.Commit); n > wCommit {
			wCommit = n
		}
	}
	var b strings.Builder
	writeRow := func(r addonListRow) {
		fmt.Fprintf(&b, "%-*s  %-*s  %-*s  %-*s  %s\n",
			wName, r.Name, wVer, r.Version, wState, r.State, wCommit, r.Commit, r.Source)
	}
	writeRow(header)
	for _, r := range rows {
		writeRow(r)
	}
	return b.String()
}

// --- status -----------------------------------------------------------------

func runStatus(ctx context.Context, env *addonEnv, args []string) int {
	_ = ctx
	if len(args) < 1 {
		fmt.Fprintln(env.Stderr, "Usage: sciclaw addon status <name>")
		return 2
	}
	name := strings.TrimSpace(args[0])
	if name == "" {
		fmt.Fprintln(env.Stderr, "Usage: sciclaw addon status <name>")
		return 2
	}
	entry, err := env.entryByName(name)
	if err != nil {
		fmt.Fprintf(env.Stderr, "Error reading registry: %v\n", err)
		return 1
	}
	if entry == nil {
		fmt.Fprintf(env.Stderr, "addon %q is not installed\n", name)
		return 1
	}
	dir := env.Lifecycle.AddonDir(name)
	fmt.Fprintln(env.Stdout, formatStatusReport(name, dir, entry))

	// Integrity check is best-effort: we only run it when the manifest is
	// readable, otherwise the registry fields alone are still useful.
	manifestPath := filepath.Join(dir, "addon.json")
	m, merr := addons.ParseManifest(manifestPath)
	if merr != nil {
		fmt.Fprintf(env.Stderr, "warning: manifest at %s is unreadable: %v\n", manifestPath, merr)
		return 0
	}
	var bootstrapPath, sidecarPath string
	if m.Bootstrap.Install != "" {
		bootstrapPath = resolveUnderDir(dir, m.Bootstrap.Install)
	}
	if m.Sidecar.Binary != "" {
		primary := resolveUnderDir(dir, m.Sidecar.Binary)
		if info, err := os.Lstat(primary); err == nil && info.Mode().IsRegular() {
			sidecarPath = primary
		} else {
			alt := filepath.Join(dir, "bin", m.Sidecar.Binary)
			if info, err := os.Lstat(alt); err == nil && info.Mode().IsRegular() {
				sidecarPath = alt
			}
		}
	}
	if err := addons.VerifyEntry(dir, entry, manifestPath, bootstrapPath, sidecarPath); err != nil {
		fmt.Fprintf(env.Stdout, "\nIntegrity: FAIL\n%v\n", err)
		return 0
	}
	fmt.Fprintln(env.Stdout, "\nIntegrity: OK")
	return 0
}

// resolveUnderDir resolves a manifest-declared relative path under dir.
// Duplicates the unexported addons.resolveUnder helper so we don't touch
// pkg/addons.
func resolveUnderDir(dir, p string) string {
	p = strings.TrimSpace(p)
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, filepath.FromSlash(p))
}

func formatStatusReport(name, dir string, entry *addons.RegistryEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Addon: %s\n", name)
	fmt.Fprintf(&b, "  Directory:        %s\n", dir)
	fmt.Fprintf(&b, "  Version:          %s\n", entry.Version)
	fmt.Fprintf(&b, "  State:            %s\n", entry.State)
	fmt.Fprintf(&b, "  Source:           %s\n", entry.Source)
	fmt.Fprintf(&b, "  Installed at:     %s\n", entry.InstalledAt)
	fmt.Fprintf(&b, "  Installed commit: %s\n", entry.InstalledCommit)
	fmt.Fprintf(&b, "  Manifest SHA256:  %s\n", displayOrDash(entry.ManifestSHA256))
	fmt.Fprintf(&b, "  Bootstrap SHA256: %s\n", displayOrDash(entry.BootstrapSHA256))
	fmt.Fprintf(&b, "  Sidecar SHA256:   %s\n", displayOrDash(entry.SidecarSHA256))
	fmt.Fprintf(&b, "  Track:            %s\n", strOrDash(entry.Track))
	fmt.Fprintf(&b, "  Signed tag:       %s\n", strOrDash(entry.SignedTag))
	fmt.Fprintf(&b, "  Signature verified: %t\n", entry.SignatureVerified)
	fmt.Fprintf(&b, "  Previous commit:  %s\n", strOrDash(entry.PreviousCommit))
	return b.String()
}

func strOrDash(p *string) string {
	if p == nil || *p == "" {
		return "-"
	}
	return *p
}

func displayOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// --- install ----------------------------------------------------------------

func runInstall(ctx context.Context, env *addonEnv, args []string) int {
	if len(args) < 1 {
		addonInstallHelp(env.Stdout)
		return 2
	}
	if args[0] == "-h" || args[0] == "--help" {
		addonInstallHelp(env.Stdout)
		return 0
	}
	source := args[0]
	flags, err := parseInstallFlags(args[1:])
	if errors.Is(err, errShowHelp) {
		addonInstallHelp(env.Stdout)
		return 0
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "Error: %v\n", err)
		addonInstallHelp(env.Stderr)
		return 2
	}

	fmt.Fprintf(env.Stdout, "Installing addon from: %s\n", source)
	fmt.Fprintf(env.Stdout, "  Pinning:  %s\n", flags.describeRef())
	if flags.Name != "" {
		fmt.Fprintf(env.Stdout, "  Expected name: %s\n", flags.Name)
	}
	fmt.Fprintf(env.Stdout, "  Will run: git clone → validate manifest → pin commit → run bootstrap.install → register\n")
	if !promptYes(env.Stdin, env.Stdout, "Proceed?", flags.Yes) {
		fmt.Fprintln(env.Stdout, "Aborted.")
		return 1
	}

	entry, err := env.Lifecycle.Install(ctx, addons.InstallOptions{
		Name:   flags.Name,
		Source: source,
		Ref:    flags.toInstallRef(),
	})
	if err != nil {
		fmt.Fprintf(env.Stderr, "Install failed: %v\n", err)
		printInstallFailureHint(env.Stderr, err)
		return 1
	}
	triggerGatewayReload(env.Stderr)

	name := flags.Name
	if name == "" {
		name = "<addon>"
	}
	fmt.Fprintln(env.Stdout)
	fmt.Fprintln(env.Stdout, formatStatusReport(name, env.Lifecycle.AddonDir(name), entry))
	fmt.Fprintf(env.Stdout, "Next: %s addon enable %s\n", invokedCLIName(), name)
	return 0
}

func addonInstallHelp(w io.Writer) {
	cn := invokedCLIName()
	fmt.Fprintf(w, "Usage: %s addon install <source> [flags]\n\n", cn)
	fmt.Fprintln(w, "Clone a git repository, validate the addon manifest, pin a commit,")
	fmt.Fprintln(w, "run the optional bootstrap.install hook, and record the registry entry.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Source can be any git URL accepted by 'git clone'.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --commit <sha>       Pin to exact commit SHA")
	fmt.Fprintln(w, "  --version <ver>      Pin to a version tag (e.g. v0.1.0)")
	fmt.Fprintln(w, "  --track <branch>     Track a branch (records branch name, still pins SHA)")
	fmt.Fprintln(w, "  --name <addon-name>  Expected addon name (fails if addon.json disagrees)")
	fmt.Fprintln(w, "  --yes, -y            Skip confirmation prompt")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "If none of --commit / --version / --track is set, the latest signed tag is used.")
}

// printInstallFailureHint emits actionable suggestions for common install
// failure modes. Best-effort classification over the error message.
func printInstallFailureHint(w io.Writer, err error) {
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "requires") && strings.Contains(lower, "on path"):
		fmt.Fprintln(w, "Hint: install the missing binary listed above and re-run the install.")
	case strings.Contains(lower, "no signed tags"):
		fmt.Fprintln(w, "Hint: pass --commit <sha>, --version <tag>, or --track <branch> to pin explicitly.")
	case strings.Contains(lower, "git not found"):
		fmt.Fprintln(w, "Hint: install git (e.g. brew install git / apt install git) and retry.")
	case strings.Contains(lower, "already installed"):
		fmt.Fprintln(w, "Hint: use 'sciclaw addon upgrade' to change versions, or 'sciclaw addon uninstall' first.")
	case strings.Contains(lower, "platform"):
		fmt.Fprintln(w, "Hint: this addon does not support your OS; check its addon.json requires.platform list.")
	}
}

// --- enable / disable -------------------------------------------------------

func runEnable(ctx context.Context, env *addonEnv, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(env.Stderr, "Usage: sciclaw addon enable <name>")
		return 2
	}
	name := strings.TrimSpace(args[0])
	entry, err := env.Lifecycle.Enable(ctx, name)
	if err != nil {
		fmt.Fprintf(env.Stderr, "Enable failed: %v\n", err)
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "integrity check failed") ||
			strings.Contains(lower, "commit drift") ||
			strings.Contains(lower, "sha256 mismatch") {
			fmt.Fprintf(env.Stderr, "\nHint: run 'sciclaw addon verify %s' for details, or\n", name)
			fmt.Fprintf(env.Stderr, "      'sciclaw addon upgrade %s' to re-pin against the current tree.\n", name)
		}
		return 1
	}
	triggerGatewayReload(env.Stderr)
	fmt.Fprintf(env.Stdout, "Addon %q enabled (commit %s).\n", name, shortCommit(entry.InstalledCommit))
	return 0
}

func runDisable(ctx context.Context, env *addonEnv, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(env.Stderr, "Usage: sciclaw addon disable <name>")
		return 2
	}
	name := strings.TrimSpace(args[0])
	entry, err := env.Lifecycle.Disable(ctx, name)
	if err != nil {
		fmt.Fprintf(env.Stderr, "Disable failed: %v\n", err)
		return 1
	}
	triggerGatewayReload(env.Stderr)
	fmt.Fprintf(env.Stdout, "Addon %q disabled (state=%s).\n", name, entry.State)
	return 0
}

// --- uninstall --------------------------------------------------------------

func runUninstall(ctx context.Context, env *addonEnv, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(env.Stderr, "Usage: sciclaw addon uninstall <name> [--force]")
		return 2
	}
	name := strings.TrimSpace(args[0])
	force := false
	for _, a := range args[1:] {
		switch a {
		case "--force", "-f":
			force = true
		case "-h", "--help":
			fmt.Fprintln(env.Stdout, "Usage: sciclaw addon uninstall <name> [--force]")
			fmt.Fprintln(env.Stdout, "  --force  Skip confirmation and also remove enabled addons.")
			return 0
		default:
			fmt.Fprintf(env.Stderr, "unknown flag: %s\n", a)
			return 2
		}
	}

	if !force {
		fmt.Fprintf(env.Stdout, "About to uninstall %q. This removes the addon directory and the registry entry.\n", name)
		if !promptTypedName(env.Stdin, env.Stdout, name) {
			fmt.Fprintln(env.Stdout, "Aborted.")
			return 1
		}
	}

	if err := env.Lifecycle.Uninstall(ctx, name, force); err != nil {
		fmt.Fprintf(env.Stderr, "Uninstall failed: %v\n", err)
		return 1
	}
	triggerGatewayReload(env.Stderr)
	fmt.Fprintf(env.Stdout, "Addon %q uninstalled.\n", name)
	return 0
}

// --- upgrade ----------------------------------------------------------------

func runUpgrade(ctx context.Context, env *addonEnv, args []string) int {
	if len(args) < 1 {
		addonUpgradeHelp(env.Stdout)
		return 2
	}
	if args[0] == "-h" || args[0] == "--help" {
		addonUpgradeHelp(env.Stdout)
		return 0
	}
	name := strings.TrimSpace(args[0])
	flags, err := parseInstallFlags(args[1:])
	if errors.Is(err, errShowHelp) {
		addonUpgradeHelp(env.Stdout)
		return 0
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "Error: %v\n", err)
		addonUpgradeHelp(env.Stderr)
		return 2
	}

	prev, err := env.entryByName(name)
	if err != nil {
		fmt.Fprintf(env.Stderr, "Error reading registry: %v\n", err)
		return 1
	}
	if prev == nil {
		fmt.Fprintf(env.Stderr, "addon %q is not installed\n", name)
		return 1
	}

	fmt.Fprintf(env.Stdout, "Upgrading addon: %s\n", name)
	fmt.Fprintf(env.Stdout, "  Current commit: %s\n", prev.InstalledCommit)
	fmt.Fprintf(env.Stdout, "  Pinning:        %s\n", flags.describeRef())
	if !promptYes(env.Stdin, env.Stdout, "Proceed?", flags.Yes) {
		fmt.Fprintln(env.Stdout, "Aborted.")
		return 1
	}

	updated, err := env.Lifecycle.Upgrade(ctx, name, flags.toInstallRef())
	if err != nil {
		if errors.Is(err, addons.ErrAlreadyAtCommit) {
			fmt.Fprintf(env.Stdout, "already at %s, nothing to do\n", prev.InstalledCommit)
			return 0
		}
		fmt.Fprintf(env.Stderr, "Upgrade failed: %v\n", err)
		return 1
	}
	triggerGatewayReload(env.Stderr)

	fmt.Fprintln(env.Stdout)
	fmt.Fprintln(env.Stdout, "Upgrade diff:")
	fmt.Fprintf(env.Stdout, "  commit:    %s → %s\n", shortCommit(prev.InstalledCommit), shortCommit(updated.InstalledCommit))
	fmt.Fprintf(env.Stdout, "  version:   %s → %s\n", prev.Version, updated.Version)
	fmt.Fprintf(env.Stdout, "  manifest:  %s → %s\n", shortCommit(prev.ManifestSHA256), shortCommit(updated.ManifestSHA256))
	fmt.Fprintf(env.Stdout, "  bootstrap: %s → %s\n", displayOrDash(shortCommit(prev.BootstrapSHA256)), displayOrDash(shortCommit(updated.BootstrapSHA256)))
	fmt.Fprintf(env.Stdout, "  sidecar:   %s → %s\n", displayOrDash(shortCommit(prev.SidecarSHA256)), displayOrDash(shortCommit(updated.SidecarSHA256)))
	fmt.Fprintln(env.Stdout)
	fmt.Fprintf(env.Stdout, "Addon %q upgraded.\n", name)
	return 0
}

func addonUpgradeHelp(w io.Writer) {
	cn := invokedCLIName()
	fmt.Fprintf(w, "Usage: %s addon upgrade <name> [flags]\n\n", cn)
	fmt.Fprintln(w, "Advance an installed addon to a new commit. If no pinning flag is")
	fmt.Fprintln(w, "passed, the prior strategy (track → auto-latest → signed tag) is reused.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --commit <sha>    Pin to exact commit SHA")
	fmt.Fprintln(w, "  --version <ver>   Pin to a version tag")
	fmt.Fprintln(w, "  --track <branch>  Follow a branch head")
	fmt.Fprintln(w, "  --yes, -y         Skip confirmation prompt")
}

// --- verify -----------------------------------------------------------------

func runVerify(ctx context.Context, env *addonEnv, args []string) int {
	_ = ctx
	if len(args) < 1 {
		fmt.Fprintln(env.Stderr, "Usage: sciclaw addon verify <name>")
		return 2
	}
	name := strings.TrimSpace(args[0])
	entry, err := env.entryByName(name)
	if err != nil {
		fmt.Fprintf(env.Stderr, "Error reading registry: %v\n", err)
		return 1
	}
	if entry == nil {
		fmt.Fprintf(env.Stderr, "addon %q is not installed\n", name)
		return 1
	}

	dir := env.Lifecycle.AddonDir(name)
	manifestPath := filepath.Join(dir, "addon.json")
	manifest, err := addons.ParseManifest(manifestPath)
	if err != nil {
		fmt.Fprintf(env.Stderr, "manifest: FAIL (%v)\n", err)
		return 1
	}

	// Check each component individually so we can print pass/fail per row
	// with expected/actual on mismatch.
	fmt.Fprintf(env.Stdout, "Verifying addon %q at %s\n", name, dir)

	ok := true

	fmt.Fprintf(env.Stdout, "  git HEAD ...............  ")
	if head, err := gitRevParseHead(dir); err != nil {
		fmt.Fprintf(env.Stdout, "FAIL (%v)\n", err)
		ok = false
	} else if head != entry.InstalledCommit {
		fmt.Fprintf(env.Stdout, "FAIL\n    expected: %s\n    actual:   %s\n", entry.InstalledCommit, head)
		ok = false
	} else {
		fmt.Fprintln(env.Stdout, "OK")
	}

	fmt.Fprintf(env.Stdout, "  manifest SHA256 ........  ")
	if got, err := addons.HashFile(manifestPath); err != nil {
		fmt.Fprintf(env.Stdout, "FAIL (%v)\n", err)
		ok = false
	} else if got != entry.ManifestSHA256 {
		fmt.Fprintf(env.Stdout, "FAIL\n    expected: %s\n    actual:   %s\n", entry.ManifestSHA256, got)
		ok = false
	} else {
		fmt.Fprintln(env.Stdout, "OK")
	}

	if entry.BootstrapSHA256 != "" && manifest.Bootstrap.Install != "" {
		fmt.Fprintf(env.Stdout, "  bootstrap SHA256 .......  ")
		bp := resolveUnderDir(dir, manifest.Bootstrap.Install)
		if got, err := addons.HashPath(bp); err != nil {
			fmt.Fprintf(env.Stdout, "FAIL (%v)\n", err)
			ok = false
		} else if got != entry.BootstrapSHA256 {
			fmt.Fprintf(env.Stdout, "FAIL\n    expected: %s\n    actual:   %s\n", entry.BootstrapSHA256, got)
			ok = false
		} else {
			fmt.Fprintln(env.Stdout, "OK")
		}
	}

	if entry.SidecarSHA256 != "" && manifest.Sidecar.Binary != "" {
		fmt.Fprintf(env.Stdout, "  sidecar SHA256 .........  ")
		sp := resolveUnderDir(dir, manifest.Sidecar.Binary)
		if info, err := os.Lstat(sp); err != nil || !info.Mode().IsRegular() {
			alt := filepath.Join(dir, "bin", manifest.Sidecar.Binary)
			if info, err := os.Lstat(alt); err == nil && info.Mode().IsRegular() {
				sp = alt
			}
		}
		if got, err := addons.HashFile(sp); err != nil {
			fmt.Fprintf(env.Stdout, "FAIL (%v)\n", err)
			ok = false
		} else if got != entry.SidecarSHA256 {
			fmt.Fprintf(env.Stdout, "FAIL\n    expected: %s\n    actual:   %s\n", entry.SidecarSHA256, got)
			ok = false
		} else {
			fmt.Fprintln(env.Stdout, "OK")
		}
	}

	if ok {
		fmt.Fprintln(env.Stdout, "All checks passed.")
		return 0
	}
	fmt.Fprintln(env.Stderr, "verify: one or more checks failed")
	return 1
}

func gitRevParseHead(dir string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git not found on PATH: %w", err)
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD in %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// --- rollback ---------------------------------------------------------------

func runRollback(ctx context.Context, env *addonEnv, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(env.Stderr, "Usage: sciclaw addon rollback <name> [--yes]")
		return 2
	}
	name := strings.TrimSpace(args[0])
	yes := false
	for _, a := range args[1:] {
		switch a {
		case "--yes", "-y":
			yes = true
		case "-h", "--help":
			fmt.Fprintln(env.Stdout, "Usage: sciclaw addon rollback <name> [--yes]")
			fmt.Fprintln(env.Stdout, "  Reverts the addon to the commit recorded in its previous_commit field.")
			fmt.Fprintln(env.Stdout, "  Only one level of rollback history is kept.")
			return 0
		default:
			fmt.Fprintf(env.Stderr, "unknown flag: %s\n", a)
			return 2
		}
	}

	entry, err := env.entryByName(name)
	if err != nil {
		fmt.Fprintf(env.Stderr, "Error reading registry: %v\n", err)
		return 1
	}
	if entry == nil {
		fmt.Fprintf(env.Stderr, "addon %q is not installed\n", name)
		return 1
	}
	if entry.PreviousCommit == nil || strings.TrimSpace(*entry.PreviousCommit) == "" {
		fmt.Fprintf(env.Stderr, "addon %q: no previous_commit recorded; upgrade at least once before rolling back\n", name)
		return 1
	}

	fmt.Fprintf(env.Stdout, "Rolling back addon: %s\n", name)
	fmt.Fprintf(env.Stdout, "  Current commit:  %s\n", entry.InstalledCommit)
	fmt.Fprintf(env.Stdout, "  Previous commit: %s\n", *entry.PreviousCommit)
	if !promptYes(env.Stdin, env.Stdout, "Proceed?", yes) {
		fmt.Fprintln(env.Stdout, "Aborted.")
		return 1
	}

	// Rollback uses the real Store rooted at sciclawHomeDir(); tests that
	// exercise this path without a real addons tree should stop at the
	// confirmation prompt or seed the registry on disk.
	store := addons.NewStore(sciclawHomeDir())
	rollbacker := &addons.Rollbacker{
		Store:    store,
		Runner:   addons.DefaultRunner{},
		AddonDir: env.Lifecycle.AddonDir,
		Now:      time.Now,
	}

	updated, err := rollbacker.Rollback(ctx, name)
	if err != nil {
		fmt.Fprintf(env.Stderr, "Rollback failed: %v\n", err)
		return 1
	}
	triggerGatewayReload(env.Stderr)
	fmt.Fprintf(env.Stdout, "Addon %q rolled back.\n", name)
	fmt.Fprintf(env.Stdout, "  commit:  %s → %s\n", shortCommit(entry.InstalledCommit), shortCommit(updated.InstalledCommit))
	fmt.Fprintf(env.Stdout, "  version: %s → %s\n", entry.Version, updated.Version)
	return 0
}

// --- sbom -------------------------------------------------------------------

func runSBOM(ctx context.Context, env *addonEnv, args []string) int {
	_ = ctx
	if len(args) < 1 {
		fmt.Fprintln(env.Stderr, "Usage: sciclaw addon sbom <name> [--output <path>]")
		return 2
	}
	name := strings.TrimSpace(args[0])
	output := ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--output", "-o":
			if i+1 >= len(args) {
				fmt.Fprintln(env.Stderr, "--output requires a value")
				return 2
			}
			output = args[i+1]
			i++
		case "-h", "--help":
			fmt.Fprintln(env.Stdout, "Usage: sciclaw addon sbom <name> [--output <path>]")
			return 0
		default:
			fmt.Fprintf(env.Stderr, "unknown flag: %s\n", args[i])
			return 2
		}
	}

	// SBOM export reads from the real Store (the pkg/addons helper takes a
	// *Store directly). Tests that exercise this path should seed a temp
	// registry; the fake-env path is covered by the flag-parsing tests.
	store := addons.NewStore(sciclawHomeDir())
	dir := env.Lifecycle.AddonDir(name)
	manifest, err := addons.ParseManifest(filepath.Join(dir, "addon.json"))
	if err != nil {
		fmt.Fprintf(env.Stderr, "Error parsing manifest for %s: %v\n", name, err)
		return 1
	}

	sbom, err := addons.Export(store, manifest, name, runtime.GOOS+"/"+runtime.GOARCH, time.Now)
	if err != nil {
		fmt.Fprintf(env.Stderr, "Error exporting SBOM: %v\n", err)
		return 1
	}

	target := env.Stdout
	if output != "" {
		if dir := filepath.Dir(output); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				fmt.Fprintf(env.Stderr, "creating output dir %s: %v\n", dir, err)
				return 1
			}
		}
		f, err := os.Create(output)
		if err != nil {
			fmt.Fprintf(env.Stderr, "creating %s: %v\n", output, err)
			return 1
		}
		defer f.Close()
		target = f
	}
	if err := addons.WriteSBOM(target, sbom); err != nil {
		fmt.Fprintf(env.Stderr, "writing SBOM: %v\n", err)
		return 1
	}
	if output != "" {
		fmt.Fprintf(env.Stderr, "SBOM written to %s\n", output)
	}
	return 0
}

// writeSBOMToTarget sends the SBOM to stdout when output is empty, otherwise
// creates (and truncates) the file at output. Retained for tests that
// exercise both destinations.
func writeSBOMToTarget(sbom *addons.SBOM, output string) error {
	if strings.TrimSpace(output) == "" {
		return addons.WriteSBOM(os.Stdout, sbom)
	}
	if dir := filepath.Dir(output); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating output dir %s: %w", dir, err)
		}
	}
	f, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("creating %s: %w", output, err)
	}
	defer f.Close()
	return addons.WriteSBOM(f, sbom)
}

// --- package-level assertions ----------------------------------------------

// Compile-time assertion: *addons.Lifecycle satisfies the CLI interface so
// drift in pkg/addons method signatures fails the build, not test-time.
var _ addonLifecycle = (*addons.Lifecycle)(nil)
