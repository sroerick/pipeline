# pakkun

`pakkun` is a project-local CLI for running named transformation pipelines while keeping intermediate artifacts out of the working tree.

It stores declared outputs under `.pipe/objects`, records runs and provenance in `.pipe/db.sqlite`, and lets you inspect or materialize artifacts later with human-readable refs.

The canonical user-edited pipeline definition lives in `pipe.yaml` at the project root.

## human written note

pakkun is vibeslop. I'm just publishing it so that I have access to it to install from Github. Right now, the only thing I've actually used it for is making cute LaTeX greeting cards. It lets me keep a clean folder and hide all the intermediate .tex files inside a hidden folder.  

Structurally, this is git for ETL pipelines. You configure your pipeline stages in YAML, those can be basically any executable. You can track runs and stuff. It's really not much more than a Makefile right now, but I like it.

I have a few more use cases I'd like to run this through and I think it may become slightly useful.

## Current status

This repository implements the v1 command set from [spec.md](./spec.md):

- `pakkun init`
- `pakkun run [pipeline]`
- `pakkun stages [pipeline-or-run]`
- `pakkun status`
- `pakkun show <ref>`
- `pakkun mount <ref> <dir>`
- `pakkun publish <ref> <path>`
- `pakkun log [pipeline-or-run]`
- `pakkun provenance <ref>`
- `pakkun ui`

The short command list undersells the current implementation somewhat. The
repository also includes:

- multiple runnable example projects under [`examples/`](./examples/)
- internal tests for CLI flows, config loading, refs, and example spec loading
- content-addressed artifact storage with publish and provenance inspection
- an experimental localhost web UI for browsing runs, artifacts, and provenance
- pipeline inheritance with `extends` for shared build graphs
- artifact reuse via `inputs[].ref` and built-in comparison steps via `kind: assert`

The main limits are about scope, not whether the basic workflow exists:

- `pakkun` runs locally on the machine or CI runner that invokes it
- metadata currently depends on the external `sqlite3` binary
- `pakkun` does not yet provide remote execution, hosted runners, or cross-job
  workflow orchestration

## Experimental web UI

`pakkun ui` starts a localhost-only web interface for the current initialized
project. The UI is intentionally scoped to the same local data model as the CLI:

- overview of pipelines, aliases, latest runs, and failed steps
- pipeline detail with declared steps and publish targets
- run detail with per-step status, captured stdout/stderr, and manifest JSON
- artifact detail with provenance, safe text previews, download, and publish

From an initialized project root:

```bash
pakkun ui
```

By default it binds to `127.0.0.1` on a random free port and prints the URL.

## Build

Requirements:

- Go 1.26+
- `sqlite3` on `PATH`

Build the CLI:

```bash
go build ./cmd/pakkun
```

The current implementation uses the local `sqlite3` binary for metadata access, so that binary must be installed anywhere you run `pakkun`.

OpenBSD build and install notes for a server build box live in
[`docs/openbsd-build.md`](./docs/openbsd-build.md).
For a generic bare-repo hook pattern, see
[`docs/post-receive-deploy.md`](./docs/post-receive-deploy.md).

## CI usage

`pakkun` is suitable for CI today if you treat it as the project-local build graph
and artifact/provenance layer.

A good fit looks like this:

- your CI platform chooses the runner OS and machine
- each job invokes `pakkun run <pipeline>`
- final outputs are materialized with declared `publish` paths or explicit
  `pakkun publish` calls
- `pakkun log`, `pakkun show`, and `pakkun provenance` are used for failure analysis

That means `pakkun` can already be used for:

- release packaging jobs on Linux and Windows
- reproducible plugin or docs build pipelines
- local dogfooding of the exact commands that CI will later run

What `pipe` is not trying to be, at least in v1:

- a CI hosting platform
- a scheduler across multiple machines
- a replacement for workflow-level matrix or fan-out/fan-in features

## Quick start

The simplest runnable example lives in [`examples/text`](./examples/text).

From the repository root:

```bash
go build -o ./pakkun ./cmd/pakkun
```

Then move into an example directory and use that binary against the local `pipe.yaml` in that directory:

```bash
cd examples/text
../../pakkun init
../../pakkun run text-demo
../../pakkun stages text-demo
../../pakkun show text-demo:upper/result
../../pakkun publish text-demo:upper/result ./build/result.txt
../../pakkun provenance text-demo:upper/result
```

What each command is doing:

- `pakkun init` creates the local `.pipe/` storage and metadata directory in the example folder.
- `pakkun run text-demo` executes the named pipeline in dependency order.
- `pakkun stages text-demo` shows the declared steps and outputs from `pipe.yaml`.
- `pakkun show text-demo:upper/result` resolves the latest successful `result` artifact and shows where it is stored under `.pipe/objects`.
- `pakkun publish ... ./build/result.txt` materializes that stored artifact back into a normal visible file.
- `pakkun provenance ...` shows which run and step produced the artifact and which prior artifacts fed into it.

If you do not want to build a binary first, you can run the CLI directly with Go:

```bash
cd examples/text
go run ../../cmd/pakkun init
go run ../../cmd/pakkun run text-demo
```

After `run`, the working tree stays mostly clean:

- declared artifacts are stored under `.pipe/objects/sha256/...`
- per-run logs and manifests live under `.pipe/runs/<run-id>/`
- the latest run is addressable as `alias:current`
- outputs that declare `publish: <relative-path>` in `pipe.yaml` are materialized into the project tree after a successful run

## Using the examples

Each example directory is its own `pakkun` project.

The normal flow is:

```bash
cd examples/<name>
../../pakkun init
../../pakkun status
../../pakkun stages
../../pakkun run <pipeline-name>
../../pakkun log
../../pakkun show <pipeline-name>:<step>/<output>
```

Things that matter:

- Run `pakkun init` inside the example directory, not at the repo root, unless you want the repo root itself to become a `pakkun` project.
- `pakkun` always looks for `pipe.yaml` in the current project root.
- `pakkun stages` without an argument only works when the spec contains a single pipeline.
- Refs like `text-demo:upper/result` point at the latest successful run of that pipeline.
- `alias:current` points at the most recent run, regardless of pipeline.

Useful inspection commands after a run:

```bash
../../pakkun status
../../pakkun log
../../pakkun show alias:current
../../pakkun show text-demo:upper
../../pakkun show text-demo:upper/result
../../pakkun provenance text-demo:upper/result
../../pakkun mount text-demo:upper ./mounted
../../pakkun publish text-demo:upper/result ./build/result.txt
```

What you should expect:

- `mount` creates a directory containing one file per declared output on that stage.
- `publish` writes one specific stored artifact back to a stable user-visible path.
- `publish` requires an artifact ref such as `text-demo:upper/result`, not a run ref or bare stage ref.
- `show` does not print file contents; it shows metadata and the resolved stored path.

## Ref model

Supported refs:

```text
<pipeline>:<step>
<pipeline>:<step>/<output>
run:<run-id>
run:<run-id>:<step>
run:<run-id>:<step>/<output>
alias:<name>
```

Examples:

```text
text-demo:upper
text-demo:upper/result
run:20260311_120000_000000000:upper/result
alias:current
```

## Step environment

The runner injects:

- `PIPE_PROJECT_ROOT`
- `PIPE_RUN_ID`
- `PIPE_STEP_NAME`
- `PIPE_STEP_OUT`
- `PIPE_INPUT_<name-or-output-name>` for prior-step inputs declared with `from` or `ref`

If an output is declared as `name: typed-ast`, the input env var becomes `PIPE_INPUT_typed_ast`.

If an input declaration includes `name: baseline`, that input becomes `PIPE_INPUT_baseline`.

## Reuse And Verification

`pipe.yaml` can define a shared build pipeline and a derived verification pipeline in the same project.

```yaml
version: 1

pipelines:
  - name: build
    steps:
      - name: render
        kind: shell
        run: cat input.txt > "$PIPE_STEP_OUT/out.txt"
        inputs:
          - source: input.txt
        outputs:
          - name: text
            path: out.txt
            type: file

  - name: verify
    extends: build
    steps:
      - name: compare
        kind: assert
        inputs:
          - from: render/text
          - ref: build:render/text
        assert:
          trim_space: true
        outputs:
          - name: report
            path: report.txt
            type: file
            publish: out/report.txt
```

That shape is useful for parity checks such as comparing a newly generated dump to a cached reference artifact without pushing anything to production.

## Publish behavior

`pipe` supports two ways to materialize managed artifacts back into the visible project tree:

- Manual publish with `pipe publish <artifact-ref> <path>`
- Automatic publish during `pipe run` when an output declares `publish: <relative-path>` in `pipe.yaml`

Example output declaration:

```yaml
outputs:
  - name: result
    path: result.txt
    type: file
    publish: build/result.txt
```

The `publish` target must stay within the project root. During `run`, any published paths are printed in the command summary.

## Example pipelines

- [`examples/text`](./examples/text): small, fully runnable text-processing pipeline.
- [`examples/compiler`](./examples/compiler): compiler-shaped pipeline with parse/typecheck/codegen stages using standard shell tools.
- [`examples/quotes`](./examples/quotes): script-driven quote-card PDF pipeline using `python3`, `patch`, and `pdflatex`.
- [`examples/latex`](./examples/latex): LaTeX-style multi-stage template matching the original spec. This one expects TeX tools such as `pdflatex` and `bibtex`.

Suggested order:

1. Start with `examples/text`.
2. Move to `examples/compiler` once the ref model makes sense.
3. Try `examples/quotes` if you want a more real script-driven pipeline with a single final PDF artifact.
4. Use `examples/latex` only if you have the TeX toolchain installed.

## Storage layout

`pipe init` creates:

```text
.pipe/
├── aliases/
├── config.yaml
├── db.sqlite
├── mounts/
├── objects/
└── runs/
```

`pipe` stores content-addressed files and directories under `.pipe/objects/sha256`, plus run manifests and step stdout/stderr under `.pipe/runs/<run-id>/`.

## Notes

- `mount` defaults to symlinks and falls back to copies when needed.
- `publish` defaults to copy mode; config loading also accepts legacy `expose_mode` and `projection_mode` keys for compatibility.
- The CLI resolves `<pipeline>:<step>` refs against the latest successful run of that pipeline.
- Output paths are constrained to the per-step output directory; a step cannot declare outputs outside `PIPE_STEP_OUT`.
