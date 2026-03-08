#!/usr/bin/env bash
set -euo pipefail

tag="${1:?usage: create-release.sh <tag> <repo> <prerelease> <formula_name> <formula_class> <source_asset_name> <build_dir> <primary_binary_name> <legacy_binary_name>}"
repo="${2:?usage: create-release.sh <tag> <repo> <prerelease> <formula_name> <formula_class> <source_asset_name> <build_dir> <primary_binary_name> <legacy_binary_name>}"
prerelease="${3:?usage: create-release.sh <tag> <repo> <prerelease> <formula_name> <formula_class> <source_asset_name> <build_dir> <primary_binary_name> <legacy_binary_name>}"
formula_name="${4:?usage: create-release.sh <tag> <repo> <prerelease> <formula_name> <formula_class> <source_asset_name> <build_dir> <primary_binary_name> <legacy_binary_name>}"
formula_class="${5:?usage: create-release.sh <tag> <repo> <prerelease> <formula_name> <formula_class> <source_asset_name> <build_dir> <primary_binary_name> <legacy_binary_name>}"
source_asset_name="${6:?usage: create-release.sh <tag> <repo> <prerelease> <formula_name> <formula_class> <source_asset_name> <build_dir> <primary_binary_name> <legacy_binary_name>}"
build_dir="${7:?usage: create-release.sh <tag> <repo> <prerelease> <formula_name> <formula_class> <source_asset_name> <build_dir> <primary_binary_name> <legacy_binary_name>}"
primary_binary_name="${8:?usage: create-release.sh <tag> <repo> <prerelease> <formula_name> <formula_class> <source_asset_name> <build_dir> <primary_binary_name> <legacy_binary_name>}"
legacy_binary_name="${9:?usage: create-release.sh <tag> <repo> <prerelease> <formula_name> <formula_class> <source_asset_name> <build_dir> <primary_binary_name> <legacy_binary_name>}"

release_created=0
tag_created=0

cleanup() {
  local status=$?
  if [ "$status" -eq 0 ]; then
    return
  fi

  echo "==> Release failed; rolling back tag/release state..."

  if gh release view "$tag" --repo "$repo" >/dev/null 2>&1; then
    gh release delete "$tag" --repo "$repo" --yes >/dev/null 2>&1 || true
  fi

  if [ "$tag_created" -eq 1 ]; then
    git push origin ":refs/tags/$tag" >/dev/null 2>&1 || true
    git tag -d "$tag" >/dev/null 2>&1 || true
  fi

  exit "$status"
}

trap cleanup EXIT

primary_assets=("${build_dir}/${primary_binary_name}"-*)
legacy_assets=("${build_dir}/${legacy_binary_name}"-*)

echo "==> Tagging ${tag}..."
git tag -a "$tag" -m "Release $tag"
tag_created=1
git push origin "$tag"

echo "==> Building source archive..."
git archive --format=tar.gz --prefix="${primary_binary_name}-${tag}/" "$tag" > "${build_dir}/${source_asset_name}"

echo "==> Generating checksums..."
(
  cd "$build_dir"
  shasum -a 256 "${primary_binary_name}"-* "${legacy_binary_name}"-* "${source_asset_name}" > sha256sums.txt
)

echo "==> Creating GitHub release..."
release_args=(
  release create "$tag"
  "${primary_assets[@]}"
  "${legacy_assets[@]}"
  "${build_dir}/${source_asset_name}"
  "${build_dir}/sha256sums.txt"
  --repo "$repo"
  --title "$tag"
  --generate-notes
)

if [ "$prerelease" = "true" ]; then
  release_args+=(--prerelease)
fi

gh "${release_args[@]}"
release_created=1

echo "==> Updating Homebrew tap..."
FORMULA_NAME="$formula_name" \
FORMULA_CLASS="$formula_class" \
SOURCE_ASSET_NAME="$source_asset_name" \
deploy/update-tap.sh "$tag" "$repo"

trap - EXIT
