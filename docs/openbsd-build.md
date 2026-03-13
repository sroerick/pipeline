# OpenBSD Build And Install

This document captures the first repeatable `pakkun` bootstrap flow for an
OpenBSD host such as `wyo.town`.

## Goal

Install a native OpenBSD `pakkun` binary for a non-root build user so project
hooks can run:

```bash
pakkun init
pakkun run <pipeline>
```

The recommended install path is:

- repo checkout: `/home/deploy/apps/pakkun`
- binary: `/home/deploy/.local/bin/pakkun`

That keeps the tool user-scoped and avoids mixing build tooling into root-owned
paths.

## Host Prerequisites

The examples below assume:

- OpenBSD amd64
- user: `deploy`
- bare git repo path: `/srv/git/pakkun.git`
- working checkout path: `/home/deploy/apps/pakkun`

Required packages and tools:

- `git`
- `go`
- `bash`
- `sqlite3`

Verify them:

```bash
command -v git
command -v go
command -v bash
command -v sqlite3
```

## One-Time Repository Bootstrap

Create the bare repository on the server:

```bash
mkdir -p /srv/git
git init --bare /srv/git/pakkun.git
```

Then add a remote from your local clone and push:

```bash
git remote add wyo deploy@wyo.town:/srv/git/pakkun.git
git push wyo master
```

If your server stores bare repositories under another owner such as `git`, keep
the published path stable and adjust ownership separately. The bootstrap contract
for the build user is just that it can clone from the bare repo.

## One-Time Build User Bootstrap

Create the local checkout and install directory:

```bash
mkdir -p /home/deploy/apps /home/deploy/.local/bin
git clone /srv/git/pakkun.git /home/deploy/apps/pakkun
```

Build and install the binary:

```bash
cd /home/deploy/apps/pakkun
go build -o /home/deploy/.local/bin/pakkun ./cmd/pakkun
```

Ensure the user can run it:

```bash
/home/deploy/.local/bin/pakkun --help
```

If `~/.local/bin` is not already on `PATH`, add this to the build user's shell
profile:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Updating After New Commits

From the existing checkout:

```bash
cd /home/deploy/apps/pakkun
git fetch origin
git checkout master
git reset --hard origin/master
go build -o /home/deploy/.local/bin/pakkun ./cmd/pakkun
```

That is the simplest repeatable update path. It is explicit, easy to debug, and
does not require root.

## Health Checks

Verify the installed binary:

```bash
pakkun --help
```

Verify it can initialize and inspect a project:

```bash
cd /path/to/a/project-with-pipe-yaml
pakkun init
pakkun stages
```

If the project already contains `.pipe/config.yaml`, omit `pakkun init`.

## Notes For Hook-Driven Deploys

For a repo such as `todo`, the deployment hook should refer to an explicit
binary path rather than assuming `PATH` is correct:

```bash
PAKKUN_BIN=/home/deploy/.local/bin/pakkun
```

That avoids shell-profile drift in non-interactive `git` hook execution.
