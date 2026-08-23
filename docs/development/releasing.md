# Releasing

A release is one tag push. `.github/workflows/release.yml` does the rest.

| What it builds | Where it goes |
|----------------|---------------|
| `marionette-server` and `marionette-agent` images, `linux/amd64` + `linux/arm64` | `ghcr.io/chunlea/marionette-{server,agent}:vX.Y.Z`, plus `:latest` unless it is a prerelease |
| `mctl` for darwin/arm64 and linux amd64/arm64 (`make dist`) | attached to the release, with `SHA256SUMS` |
| The commit list since the previous tag | the notes of a **draft** GitHub release |

The draft is where a person is still required. The workflow writes a commit
list; you rewrite it into something worth reading and press publish.

## Cutting a release

**1. Start from a green `main`.** The release workflow does not re-run the test
suite — it builds the commit you tag. Check CI is green on that commit first.

**2. Write the changelog entry.** In `CHANGELOG.md`, turn `[Unreleased]` into
`[X.Y.Z] — YYYY-MM-DD`, add a fresh empty `[Unreleased]`, and update the two
link definitions at the bottom of the file. The first draft is mechanical:

```bash
git log --no-merges --pretty='- %s' v0.1.0..HEAD
```

Commit that on `main` (a normal PR, or directly if that is how the repo is
being run).

**3. Tag and push.**

```bash
git tag -a v0.2.0 -m "v0.2.0 — one line about what this release is"
git push origin main
git push origin v0.2.0
```

The tag has to be an annotated or signed tag, and it must exist on the remote:
the workflow refuses to create a release for a tag it cannot verify.

**4. Watch it.**

```bash
gh run watch "$(gh run list --workflow=release.yml --limit=1 --json databaseId -q '.[0].databaseId')"
```

Roughly ten minutes: the arm64 agent image installs Debian packages under
emulation, which is the slow part.

**5. Publish the draft.**

```bash
gh release view v0.2.0 --web
```

Replace the generated commit list with the highlights (the changelog entry is
usually the right text), keep the image and checksum sections, publish.

**6. Verify what shipped.**

```bash
docker pull ghcr.io/chunlea/marionette-server:v0.2.0
docker image inspect ghcr.io/chunlea/marionette-server:v0.2.0 \
  -f '{{index .Config.Labels "org.opencontainers.image.version"}}'   # v0.2.0

curl -fsSL https://github.com/chunlea/marionette/releases/download/v0.2.0/mctl_v0.2.0_darwin_arm64.tar.gz | tar -xz
./mctl version                                                       # v0.2.0
```

## Rehearsing without spending a version

The workflow cannot be fully exercised by a pull request, so it has a dry run:
it builds both images and both binaries and pushes nothing.

```bash
gh workflow run release.yml --ref main -f tag=v9.9.9 -f dry_run=true
```

Run that after any change to the workflow, the Dockerfiles, or `make dist`. The
same dispatch with `-f dry_run=false`, run from the tag's ref
(`--ref v0.2.0`), is the way to re-drive a real release by hand if the tag push
did not trigger one.

## Version numbers

Semantic versioning. A prerelease is any tag with a suffix — `v0.2.0-rc1` —
and the workflow treats it accordingly: marked as a prerelease on GitHub, and
**never** moved to `:latest`. Anything that is not `vMAJOR.MINOR.PATCH` with an
optional suffix fails the workflow in its first job rather than half-publishing.

## When something fails

- **The whole thing is re-runnable.** Images re-push the same tags; the release
  job replaces the assets of an existing release rather than failing on it. Fix
  the cause and re-run the failed jobs.
- **`denied: permission_denied` on the ghcr push.** See the one-time setup
  below.
- **The draft has no commit list.** The notes come from
  `git describe --tags --abbrev=0 "$TAG^"`; with no earlier tag reachable, the
  job falls back to the whole history.

## One-time ghcr setup

The images are owned by the user `chunlea`, and v0.1.0 was pushed by hand from
a laptop. A package created that way is not attached to this repository, so the
workflow's `GITHUB_TOKEN` — which is scoped to the repository, not to the user
— is not allowed to push it. There is no API call in the workflow that can fix
that from the outside; it is a click, once per package:

1. <https://github.com/users/chunlea/packages/container/marionette-server/settings>
2. **Manage Actions access** → **Add repository** → `chunlea/marionette` →
   role **Write**.
3. Same for `marionette-agent`.

Both packages are already public, so pulls need no authentication; nothing has
to change there.

Packages created by the workflow from then on link themselves, through the
`org.opencontainers.image.source` label in the Dockerfiles.

No personal access token is involved anywhere in the release path, and none
should be introduced. The workflow authenticates with
`${{ secrets.GITHUB_TOKEN }}` under a `packages: write` permission block.

## Known limitation

`mctl version` reports the release tag. The server and agent binaries do not:
`cmd/server` and `cmd/agent` have no version variables to stamp, so the
`-X main.version=…` flags the Makefile and the Dockerfiles already pass to them
are inert (the linker ignores an `-X` for a symbol that does not exist). The
images still carry the version as an OCI label.

Closing this is a six-line change in each `main` package — the same
`version`/`commit`/`buildDate` block `cmd/mctl/version.go` has, logged at
startup — after which the existing build plumbing stamps them with no further
change.
