#!/usr/bin/env bash
# Update drpedapati/homebrew-tap formula with binary URLs and SHA-256s
# from a GitHub release. Called by `make release-local`.
#
# Usage: deploy/update-tap.sh <tag> <repo>
#   e.g.: deploy/update-tap.sh v0.1.37 drpedapati/sciclaw
set -euo pipefail

tag="${1:?Usage: update-tap.sh <tag> <repo>}"
repo="${2:?Usage: update-tap.sh <tag> <repo>}"
version="${tag#v}"
base="https://github.com/${repo}/releases/download/${tag}"
tap_repo="drpedapati/homebrew-tap"
formula_path="Formula/sciclaw.rb"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

# Download binaries and source, compute SHA-256s
echo "  Downloading binaries and source archive..."
for target in darwin-arm64 linux-arm64 linux-amd64; do
  curl -fsSL -o "${tmpdir}/sciclaw-${target}" "${base}/sciclaw-${target}"
done
source_url="https://github.com/${repo}/archive/refs/tags/${tag}.tar.gz"
curl -fsSL -o "${tmpdir}/source.tar.gz" "${source_url}"

sha_darwin_arm64=$(shasum -a 256 "${tmpdir}/sciclaw-darwin-arm64" | awk '{print $1}')
sha_linux_arm64=$(shasum -a 256 "${tmpdir}/sciclaw-linux-arm64" | awk '{print $1}')
sha_linux_amd64=$(shasum -a 256 "${tmpdir}/sciclaw-linux-amd64" | awk '{print $1}')
sha_source=$(shasum -a 256 "${tmpdir}/source.tar.gz" | awk '{print $1}')

echo "  darwin-arm64: ${sha_darwin_arm64}"
echo "  linux-arm64:  ${sha_linux_arm64}"
echo "  linux-amd64:  ${sha_linux_amd64}"
echo "  source:       ${sha_source}"

# Clone tap, render formula, push
echo "  Updating tap formula..."
gh repo clone "${tap_repo}" "${tmpdir}/tap" -- --depth 1 -q

cat > "${tmpdir}/tap/${formula_path}" <<FORMULA
class Sciclaw < Formula
  desc "Autonomous paired scientist CLI forked from PicoClaw"
  homepage "https://github.com/drpedapati/sciclaw"
  version "${version}"
  license "MIT"

  on_macos do
    on_arm do
      url "${base}/sciclaw-darwin-arm64"
      sha256 "${sha_darwin_arm64}"
    end
  end

  on_linux do
    on_arm do
      url "${base}/sciclaw-linux-arm64"
      sha256 "${sha_linux_arm64}"
    end
    on_intel do
      url "${base}/sciclaw-linux-amd64"
      sha256 "${sha_linux_amd64}"
    end
    depends_on "sciclaw-quarto"
  end

  # Source archive provides skills and workspace templates
  resource "source" do
    url "${source_url}"
    sha256 "${sha_source}"
  end

  depends_on "irl"
  depends_on "pandoc"
  depends_on "ripgrep"
  depends_on "uv"
  depends_on "sciclaw-docx-review"
  depends_on "sciclaw-pubmed-cli"

  def install
    # Install pre-compiled binary
    if OS.mac?
      bin.install "sciclaw-darwin-arm64" => "sciclaw"
    elsif OS.linux? && Hardware::CPU.arm?
      bin.install "sciclaw-linux-arm64" => "sciclaw"
    else
      bin.install "sciclaw-linux-amd64" => "sciclaw"
    end
    (bin/"picoclaw").make_symlink bin/"sciclaw"

    # Install skills and workspace templates from source
    resource("source").stage do
      pkgshare.install "skills"
      (pkgshare/"templates"/"workspace").install Dir["pkg/workspacetpl/templates/workspace/*.md"]
    end
  end

  def post_install
    return unless service_definition_installed?

    unless quiet_system(bin/"sciclaw", "service", "refresh")
      opoo "sciClaw service refresh could not be applied automatically. Run: sciclaw service refresh"
    end
  end

  def service_definition_installed?
    if OS.mac?
      (Pathname.new(Dir.home)/"Library"/"LaunchAgents"/"io.sciclaw.gateway.plist").exist?
    elsif OS.linux?
      (Pathname.new(Dir.home)/".config"/"systemd"/"user"/"sciclaw-gateway.service").exist?
    else
      false
    end
  end

  def caveats
    <<~EOS
      If you use the sciClaw background gateway service, this formula attempts
      to refresh the service automatically on upgrade.

      If your environment blocks that step (no user service session), run:
        sciclaw service refresh
    EOS
  end

  test do
    assert_match "Usage:", shell_output("#{bin}/sciclaw 2>&1", 1)
    assert_match "Usage:", shell_output("#{bin}/picoclaw 2>&1", 1)
    assert_match "v#{version}", shell_output("#{bin}/sciclaw --version")
    assert_match "ripgrep", shell_output("#{Formula["ripgrep"].opt_bin}/rg --version")
    assert_match "irl", shell_output("#{Formula["irl"].opt_bin}/irl --version 2>&1")
    if OS.linux?
      assert_match(/\d+\.\d+\.\d+/, shell_output("#{Formula["sciclaw-quarto"].opt_bin}/quarto --version").strip)
    end
    assert_match "docx-review", shell_output("#{Formula["sciclaw-docx-review"].opt_bin}/docx-review --version")
    assert_match "PubMed", shell_output("#{Formula["sciclaw-pubmed-cli"].opt_bin}/pubmed --help")
    ENV["HOME"] = testpath
    system bin/"sciclaw", "onboard", "--yes"
    assert_path_exists testpath/"sciclaw/AGENTS.md"
    assert_path_exists testpath/"sciclaw/HOOKS.md"
    assert_path_exists testpath/"sciclaw/skills/scientific-writing/SKILL.md"
  end
end
FORMULA

cd "${tmpdir}/tap"
git add "${formula_path}"
if git diff --cached --quiet; then
  echo "  No formula changes detected."
else
  git commit -m "sciclaw ${tag}" -q
  git push origin main -q
  echo "  Tap updated: ${tap_repo} → ${tag}"
fi
