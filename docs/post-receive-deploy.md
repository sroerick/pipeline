# Generic Post-Receive Deploy Example

This document captures a minimal pattern for using `pakkun` from a bare git
repository `post-receive` hook.

The goal is not to prescribe a single production layout. The goal is to show a
repeatable shape:

1. push to a deployment branch
2. update a working checkout
3. initialise `.pipe/` once if needed
4. run a CI pipeline
5. run a release pipeline
6. optionally hand the resulting artifact to a promotion command

The accompanying sample hook lives at
[`docs/examples/post-receive-pakkun.ksh`](./examples/post-receive-pakkun.ksh).

## What This Example Assumes

- the same host receives the `git push` and runs `pakkun`
- the project repository already contains a `pipe.yaml`
- a build user can write to the working checkout
- the release pipeline publishes a visible artifact path such as
  `build/release.tar.gz`

It does not assume a specific host name, service name, user name, or runtime
layout.

## Suggested Layout

- bare repo: `/path/to/repo.git`
- worktree: `/path/to/worktree`
- pakkun binary: `/path/to/pakkun`

The hook example keeps those as environment variables so the script can be
copied with minimal edits.

## Why A Worktree Instead Of Building In The Bare Repo

Building in the bare repository is fragile:

- hooks and build outputs mix with repository internals
- many tools expect a normal checkout
- cleanup gets risky

The example always updates a normal checkout and builds there.

## Preserving `.pipe`

The sample uses:

```text
git clean -fdx -e .pipe
```

That keeps `pakkun` metadata between deploys. This is useful when you want
artifact and run history to survive normal source cleanups.

If you prefer completely fresh metadata each deploy, remove that exclusion and
re-run `pakkun init`.

## Optional Promotion Step

The generic script supports:

- `RELEASE_ARTIFACT`: visible path produced by the release pipeline
- `PROMOTE_CMD`: optional command invoked after a successful release build

That separation keeps the example useful for both:

- "build only" flows
- "build then install/restart" flows

`pakkun` itself stops at producing the artifact. Promotion remains an explicit
host-specific concern.
