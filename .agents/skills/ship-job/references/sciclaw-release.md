# SciClaw release and deployment continuation

Read this only after the draft PR is verified and the user has separately authorized the applicable merge, release, or deployment operation.

## Merge gate

1. Refresh the exact PR head, review decision, mergeability, and CI.
2. Require no unresolved material review findings.
3. Merge only with explicit authorization.
4. Record the resulting `main` commit. All tags and release artifacts must derive from that commit.

## Version gate

1. Inspect the latest stable and development tags; never guess the next version.
2. Use a new patch version after any change to an earlier canary. Never move or rewrite a published tag.
3. Update release notes and public documentation when the change is user-visible.

## Release paths

Prefer the repository workflow:

```bash
make release-dispatch RELEASE_TAG=vX.Y.Z
```

For a development canary, use the repository's development release target with a fresh `-dev.N` tag. For a stable local release, `make release-local RELEASE_TAG=vX.Y.Z` requires a clean tree.

Verify the GitHub workflow created the expected tag, binaries, checksums, release, and—only for a stable non-draft release—the Homebrew tap update.

## Homebrew deployment contract

Follow `docs/ops/brew-only-deployment-contract.md` completely. On Data3, use Linuxbrew's resolved paths.

1. Snapshot the current version, resolved binary, service binding, model status, and gateway health.
2. Run `brew update` and upgrade the intended `sciclaw` or `sciclaw-dev` formula.
3. Refresh/reinstall the service using the active Brew binary, then restart it.
4. Verify the binary resolves under the Brew Cellar/opt path and the service uses the same installation.
5. Run `sciclaw doctor`, `sciclaw status`, and `sciclaw models status`.
6. Confirm Discord reconnects and execute a narrow live canary in the intended routed workspace.

Do not overwrite configuration, workspace state, credentials, routing, or memory during upgrade.

### Existing workspace guidance refresh

Onboarding does not overwrite an existing workspace `AGENTS.md`. For this release, inspect the active DEB guidance at `/home/ernie/Dropbox/sciclaw/deb-sciclaw/AGENTS.md` after taking a timestamped backup. If it contains the obsolete image-generation sentence, replace only that sentence with the new `image-generation` skill routing from the installed workspace template. Show the exact diff, verify the obsolete wording is absent, and preserve every unrelated workspace instruction. Do not replace the whole file.

Confirm the routed DEB workspace contains the released `skills/image-generation/SKILL.md`. If it is missing, use the released SciClaw skill installation/doctor repair path; do not copy it from a development checkout.

## Imagery-routing acceptance

For image-generation changes, test distinct products in a fresh DEB session:

1. standalone illustration;
2. visual asset intended for a slide;
3. editable PowerPoint improvement;
4. complete image-generated 16:9 slide using supplied PowerPoint content.

The complete-slide case must retain an action title, concise audience-facing wording, an integrated exhibit or analogy, and a clear takeaway. It must not silently return a wordless illustration or switch to editable PowerPoint.

## Rollback

Record the previous formula version and service state before deployment. If the canary fails, restore the prior Brew version using the documented contract, refresh the service, and verify gateway recovery. Do not repair a failed release with a hot-copied binary.
