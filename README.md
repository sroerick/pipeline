# pipe

`pipe` is a project-local CLI for running named transformation pipelines while keeping intermediate artifacts out of the working tree.

It stores declared outputs under `.pipe/objects`, records runs and provenance in `.pipe/db.sqlite`, and lets you inspect or materialize artifacts later with human-readable refs.

## human written note

pipe is vibeslop. I'm just publishing it so that I have access to it to install from Github. Right now, the only thing I've actually used it for is making cute LaTeX greeting cards. It lets me keep a clean folder and hide all the intermediate .tex files inside a hidden folder.  

Structurally, this is git for ETL pipelines. You configure your pipeline stages in YAML, those can be basically any executable. You can track runs and stuff. It's really not much more than a Makefile right now, but I like it.

I have a few more use cases I'd like to run this through and I think it may become slightly useful.

## Current status

This repository implements the v1 command set from [spec.md](./spec.md):

- `pipe init`
- `pipe run [pipeline]`
- `pipe stages [pipeline-or-run]`
- `pipe status`
- `pipe show <ref>`
- `pipe mount <ref> <dir>`
- `pipe publish <ref> <path>`
- `pipe log [pipeline-or-run]`
- `pipe provenance <ref>`

## Build

Requirements:

- Go 1.26+
- `sqlite3` on `PATH`

Build the CLI:

```bash
go build ./cmd/pipe
```

The current implementation uses the local `sqlite3` binary for metadata access, so that binary must be installed anywhere you run `pipe`.

## Quick start

The simplest runnable example lives in [`examples/text`](./examples/text).

From the repository root:

```bash
go build -o ./pipe ./cmd/pipe
```

Then move into an example directory and use that binary against the local `pipe.yaml` in that directory:

```bash
cd examples/text
../../pipe init
../../pipe run text-demo
../../pipe stages text-demo
../../pipe show text-demo:upper/result
../../pipe publish text-demo:upper/result ./build/result.txt
../../pipe provenance text-demo:upper/result
```

What each command is doing:

- `pipe init` creates the local `.pipe/` storage and metadata directory in the example folder.
- `pipe run text-demo` executes the named pipeline in dependency order.
- `pipe stages text-demo` shows the declared steps and outputs from `pipe.yaml`.
- `pipe show text-demo:upper/result` resolves the latest successful `result` artifact and shows where it is stored under `.pipe/objects`.
- `pipe publish ... ./build/result.txt` materializes that stored artifact back into a normal visible file.
- `pipe provenance ...` shows which run and step produced the artifact and which prior artifacts fed into it.

If you do not want to build a binary first, you can run the CLI directly with Go:

```bash
cd examples/text
go run ../../cmd/pipe init
go run ../../cmd/pipe run text-demo
```

After `run`, the working tree stays mostly clean:

- declared artifacts are stored under `.pipe/objects/sha256/...`
- per-run logs and manifests live under `.pipe/runs/<run-id>/`
- the latest run is addressable as `alias:current`
- outputs that declare `publish: <relative-path>` in `pipe.yaml` are materialized into the project tree after a successful run

## Using the examples

Each example directory is its own `pipe` project.

The normal flow is:

```bash
cd examples/<name>
../../pipe init
../../pipe status
../../pipe stages
../../pipe run <pipeline-name>
../../pipe log
../../pipe show <pipeline-name>:<step>/<output>
```

Things that matter:

- Run `pipe init` inside the example directory, not at the repo root, unless you want the repo root itself to become a `pipe` project.
- `pipe` always looks for `pipe.yaml` in the current project root.
- `pipe stages` without an argument only works when the spec contains a single pipeline.
- Refs like `text-demo:upper/result` point at the latest successful run of that pipeline.
- `alias:current` points at the most recent run, regardless of pipeline.

Useful inspection commands after a run:

```bash
../../pipe status
../../pipe log
../../pipe show alias:current
../../pipe show text-demo:upper
../../pipe show text-demo:upper/result
../../pipe provenance text-demo:upper/result
../../pipe mount text-demo:upper ./mounted
../../pipe publish text-demo:upper/result ./build/result.txt
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
- `PIPE_INPUT_<output-name>` for prior-step inputs declared with `from`

If an output is declared as `name: typed-ast`, the input env var becomes `PIPE_INPUT_typed_ast`.

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
- [`examples/latex`](./examples/latex): LaTeX-style multi-stage template matching the original spec. This one expects TeX tools such as `pdflatex` and `bibtex`.

Suggested order:

1. Start with `examples/text`.
2. Move to `examples/compiler` once the ref model makes sense.
3. Use `examples/latex` only if you have the TeX toolchain installed.

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
