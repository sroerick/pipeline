# `pipe` v1 Technical Spec

Build a portable Go CLI utility named `pipe` for tracking and inspecting multi-stage transformation pipelines. The tool should feel somewhat Git-like in ergonomics, but its purpose is not source control. Its purpose is to manage **derived artifacts**, **intermediate pipeline stages**, and **provenance**.

## 1. Product goal

`pipe` manages pipelines where a source input is transformed through multiple named stages, producing intermediate and final outputs. It should support use cases such as:

- LaTeX build chains with many intermediate files
- language/compiler pipelines with multiple IR/AST/codegen stages
- generic file transformation pipelines
- script-driven data pipelines

The core value is:

- keep the working tree clean
- store intermediate outputs in hidden managed storage
- allow users to inspect any stage later
- publish or mount specific outputs on demand
- record provenance for how an output was produced

## 2. Non-goals for v1

Do **not** implement:

- remote execution
- distributed cache
- plugin system
- GUI or TUI
- branching/merging semantics
- web UI
- specialized SQL execution engines
- workflow scheduling across machines
- background daemons

v1 should be a local, project-scoped CLI tool.

Using `pipe` inside a CI job is still in scope for v1. The non-goal is replacing
the CI platform itself. In other words: `pipe` may be the build graph and
artifact manager that a runner executes, while job scheduling, machine
selection, and cross-runner orchestration stay outside the tool.

## 3. Core concepts

### Pipeline
A named DAG of steps defined in YAML.

### Step
A named transformation with:
- execution kind
- command/script
- declared inputs
- declared outputs
- optional environment variables
- optional retention policy

### Artifact
A stored output produced by a step. Usually a file or directory in v1.

### Run
A single execution of a pipeline.

### Projection
A user-visible materialization of one or more stored artifacts, created by:
- `mount`
- `publish`

### Provenance
The recorded lineage connecting inputs, steps, outputs, and runs.

## 4. UX principles

The tool should optimize for:

- human-readable refs
- clean project directories
- inspectable intermediate state
- stable command vocabulary
- project-local storage
- simple install and distribution as a single binary

Users should think in terms of:
- pipelines
- stages
- outputs
- runs
- refs

Users should not need to know internal storage paths.

## 5. CLI commands

Implement these commands in v1:

```bash
pipe init
pipe run [pipeline]
pipe stages [pipeline-or-run]
pipe status
pipe show <ref>
pipe mount <ref> <dir>
pipe publish <ref> <path>
pipe log [pipeline-or-run]
pipe provenance <ref>
```

### `pipe init`
Initializes `.pipe/` metadata in the current project.

Behavior:
- create `.pipe/`
- create `.pipe/config.yaml`
- create `.pipe/objects/`
- create `.pipe/runs/`
- create `.pipe/aliases/`
- create `.pipe/mounts/`
- create `.pipe/db.sqlite`

### `pipe run [pipeline]`
Runs the given pipeline, or the default pipeline if omitted.

Behavior:
- load pipeline spec
- validate DAG
- execute steps in dependency order
- print per-step progress while the run is active
- capture outputs into managed storage
- write run manifest
- write metadata to SQLite
- return nonzero exit status on pipeline failure

### `pipe stages [pipeline-or-run]`
Lists steps and outputs for a pipeline definition or specific run.

### `pipe status`
Shows:
- current project root
- available pipelines
- latest run(s)
- failed steps if any
- current aliases if any
- possibly stale or missing pipeline state

Keep output simple in v1.

### `pipe show <ref>`
Shows metadata for a ref and, when sensible, prints or locates the artifact.

For v1:
- for file artifacts, print resolved stored path and metadata
- for stage refs, show step metadata and outputs
- for run refs, show run summary

Do not auto-open editors or viewers in v1.

### `pipe mount <ref> <dir>`
Creates a projection directory containing the artifacts associated with the ref.

Use symlink by default on platforms that support it. Support fallback to copy mode via config.

### `pipe publish <ref> <path>`
Materializes a single artifact at a stable user-visible path.

Example:
```bash
pipe publish thesis:latex2/pdf ./build/thesis.pdf
```

`publish` requires an artifact ref, not a run ref or a bare stage ref.

### `pipe log [pipeline-or-run]`
Shows recent runs and basic execution details.

### `pipe provenance <ref>`
Shows how the target artifact or stage was produced:
- run id
- producing step
- input refs
- output refs

For v1, text output is enough.

## 6. Ref syntax

Use human-readable refs.

Supported forms for v1:

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
thesis:latex1
thesis:latex2/pdf
compiler:typecheck/typed-ast
run:20260310_001:parse/tokens
alias:current
```

Rules:
- pipeline names must be unique within a project
- step names must be unique within a pipeline
- output names must be unique within a step

## 7. Pipeline spec format

Use YAML.

Default pipeline file name:
```text
pipe.yaml
```

### Example

```yaml
version: 1

pipelines:
  - name: thesis
    steps:
      - name: latex1
        kind: shell
        run: pdflatex -interaction=nonstopmode -output-directory "$PIPE_STEP_OUT" thesis.tex
        inputs:
          - source: thesis.tex
          - source: refs.bib
        outputs:
          - name: aux
            path: thesis.aux
            type: file
          - name: log
            path: thesis.log
            type: file
          - name: toc
            path: thesis.toc
            type: file

      - name: bibtex
        kind: shell
        run: bibtex "$PIPE_INPUT_aux"
        inputs:
          - from: latex1/aux
        outputs:
          - name: bbl
            path: thesis.bbl
            type: file
          - name: blg
            path: thesis.blg
            type: file

      - name: latex2
        kind: shell
        run: pdflatex -interaction=nonstopmode -output-directory "$PIPE_STEP_OUT" thesis.tex
        inputs:
          - source: thesis.tex
          - from: bibtex/bbl
        outputs:
          - name: pdf
            path: thesis.pdf
            type: file
          - name: log
            path: thesis.log
            type: file
```

Pipelines may also inherit shared steps from another pipeline:

```yaml
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
```

## 8. Pipeline semantics

### Step kinds
Support these values in v1:
- `shell`
- `exec`
- `assert`

You may internally treat both similarly, but keep the field for future evolution.

`assert` is a built-in comparison step for file artifacts. In v1 it compares exactly two file inputs, writes a report artifact, and fails the run if the normalized contents differ.

### Inputs
Support three kinds of inputs:
- `source`: path from the project working tree
- `from`: reference to a prior step output
- `ref`: reference to an artifact from another pipeline or prior run

Examples:

```yaml
inputs:
  - source: thesis.tex
  - from: latex1/aux
  - ref: baseline:latex2/pdf
```

Inputs may optionally provide `name`. If present, the runner exposes that input as `PIPE_INPUT_<name>`; otherwise `from` and `ref` inputs use the output name.

### Outputs
Each output declaration must include:
- `name`
- `path`
- `type`

It may also include:
- `publish`: project-relative path to materialize after a successful run

For v1, support:
- `file`
- `dir`

If `publish` is present, it must stay within the project root.

### Environment
Each step may specify:
- optional key/value env map

### Retention
Optional field:
- `ephemeral`
- `keep`
- `important`

Retention may be recorded in metadata in v1, but garbage collection does not need to be fully implemented.

### Pipeline inheritance

Pipelines may specify `extends: <pipeline-name>` to prepend all steps from the base pipeline before the derived pipeline's own steps. Step names must remain unique after expansion.

This is intended for workflows such as:

- building once, then verifying against a cached reference artifact
- keeping a production-like build flow and a local-only parity flow in the same `pipe.yaml`
- reusing extraction/build steps across multiple publish or validation pipelines

## 9. Execution model

For each run:

1. create run id
2. create run workspace
3. topologically sort steps
4. execute each step in dependency order
5. materialize inputs for the step
6. provide a per-step output directory
7. capture stdout/stderr
8. validate declared outputs exist
9. store outputs in managed object storage
10. write artifact records
11. write stage result metadata
12. write run manifest
13. publish any declared outputs
14. return summary

### Failure behavior
Default behavior:
- stop pipeline on first failed step
- mark run as failed
- preserve metadata and captured logs for completed and failed steps
- do not publish declared outputs for failed runs

## 10. Storage layout

All managed data is project-local under `.pipe/`.

```text
.pipe/
├── config.yaml
├── db.sqlite
├── objects/
│   └── sha256/
├── runs/
│   └── <run-id>/
│       ├── manifest.json
│       ├── steps/
│       │   └── <step-name>/
│       │       ├── stdout.txt
│       │       ├── stderr.txt
│       │       └── work/
├── aliases/
└── mounts/
```

### Rules
- actual stored artifacts should be content-addressed under `.pipe/objects/`
- run manifests should be stored under `.pipe/runs/<run-id>/manifest.json`
- SQLite is the main metadata index
- the working project tree should remain untouched unless the user explicitly uses `mount` or `publish`, or a successful run materializes declared `publish` targets

## 11. Artifact storage

Use content-addressed storage.

### Hashing
Use SHA-256 for v1.

### Storage
Store artifacts by content hash.

Suggested layout:
```text
.pipe/objects/sha256/ab/abcdef...
```

### Records
SQLite artifact records should map:
- run id
- step name
- output logical name
- object ref hash
- artifact type
- size
- timestamps

For directories, v1 may either:
- hash a deterministic directory snapshot
- or package directories into a tar-like blob before storing

Pick one simple approach and document it.

## 12. SQLite metadata

Use SQLite for metadata only. Do not store raw artifact blobs in SQLite.

Suggested tables:

### `runs`
Fields:
- `id`
- `pipeline_name`
- `status`
- `started_at`
- `ended_at`

### `steps`
Fields:
- `id`
- `run_id`
- `step_name`
- `status`
- `command`
- `exit_code`
- `stdout_object_ref`
- `stderr_object_ref`
- `started_at`
- `ended_at`

### `artifacts`
Fields:
- `id`
- `run_id`
- `step_name`
- `output_name`
- `artifact_type`
- `object_ref`
- `size_bytes`
- `created_at`

### `aliases`
Fields:
- `name`
- `target_ref`
- `updated_at`

### `provenance_edges`
Fields:
- `from_artifact_id`
- `to_artifact_id`
- `via_step_name`

Schema can evolve, but these concepts should exist.

## 13. Manifest format

Every run should produce a JSON manifest.

### `manifest.json`
Should include:
- run id
- pipeline name
- started/ended timestamps
- status
- list of step manifests

Each step manifest should include:
- step name
- command
- inputs
- outputs
- exit code
- stdout/stderr refs
- timestamps
- status

This manifest is meant to be human-inspectable and machine-readable.

## 14. Package layout

Use this package structure:

```text
cmd/pipe/main.go

internal/cli/
internal/config/
internal/pipeline/
internal/engine/
internal/runner/
internal/store/
internal/manifest/
internal/workspace/
internal/cache/
internal/graph/
internal/hash/
internal/fsx/
internal/db/
internal/ui/
```

### Responsibilities

#### `cli`
Argument parsing and command dispatch only.

#### `config`
Load `.pipe/config.yaml` and locate project root.

#### `pipeline`
Pipeline spec types, validation, and reference resolution.

#### `engine`
Run orchestration and stage execution flow.

#### `runner`
Process execution, env injection, stdout/stderr capture.

#### `store`
Content-addressed object storage and artifact resolution.

#### `manifest`
Runtime manifest types.

#### `workspace`
Mounting and publishing artifacts into user-visible paths.

#### `cache`
Reserved for future cache key logic. Keep minimal in v1.

#### `graph`
Topological sorting and DAG validation.

#### `hash`
File, directory, and command hashing helpers.

#### `fsx`
Filesystem utilities: atomic writes, copying, linking, path safety.

#### `db`
SQLite initialization, migrations, and queries.

#### `ui`
Text output formatting for terminal display.

## 15. Portability requirements

The implementation must be portable.

### OS expectations
Primary target:
- Linux
- macOS

Secondary target:
- Windows-compatible where practical

### Projection modes
`mount` and `publish` should support:
- symlink
- copy

`mount` may default to symlink where supported. `publish` may default to copy. Copy fallback must exist.

### Path handling
Use `filepath` and avoid hardcoded slash behavior.

### Dependencies
Keep dependencies minimal.

Recommended:
- standard library
- one YAML library
- one SQLite driver
- optional CLI helper library if desired

Avoid framework-heavy design.

## 16. Output and error behavior

### Text output
Default output should be concise and readable.

### Exit codes
- `0` on success
- nonzero on command failure, invalid config, missing refs, or execution errors

### Error messages
Errors should be explicit and actionable.

Examples:
- unknown pipeline
- unknown ref
- cycle detected in pipeline
- declared output missing after step execution
- project not initialized

## 17. Suggested internal types

These do not need to match exactly, but the architecture should reflect them.

```go
type Pipeline struct {
    Name  string
    Steps []Step
}

type Step struct {
    Name      string
    Kind      string
    Run       string
    Inputs    []InputRef
    Outputs   []OutputDecl
    Env       map[string]string
    Retention string
}

type InputRef struct {
    Source string
    From   string
}

type OutputDecl struct {
    Name    string
    Path    string
    Type    string
    Publish string
}

type RunRecord struct {
    ID        string
    Pipeline  string
    Status    string
    StartedAt time.Time
    EndedAt   time.Time
}

type ArtifactRecord struct {
    ID          string
    RunID       string
    StepName    string
    OutputName  string
    ArtifactType string
    ObjectRef   string
    SizeBytes   int64
    CreatedAt   time.Time
}
```

## 18. v1 implementation priorities

Priority order:

1. `init`
2. pipeline parsing and validation
3. `run`
4. managed storage
5. SQLite metadata
6. `stages`
7. `show`
8. `mount`
9. `publish`
10. `log`
11. `provenance`

Do not overbuild cache, retention, or extensibility before the core workflow works.

## 19. Acceptance criteria

The implementation is acceptable when all of the following work:

### Case 1: LaTeX-like pipeline
A user can define a pipeline with multiple shell steps, run it, and inspect intermediate outputs without cluttering the working tree.

### Case 2: Compiler-like pipeline
A user can define parse/typecheck/codegen stages and inspect each stage’s declared outputs by ref.

### Case 3: Projection
A user can mount or publish a chosen artifact into a visible path.

### Case 4: Provenance
A user can ask how an output was produced and see run/step lineage.

### Case 5: Clean storage model
All managed artifacts are stored under `.pipe/`, indexed in SQLite, and referenced by human-readable refs.

## 20. Summary

Build `pipe` as:

- Go CLI
- YAML pipeline specs
- SQLite metadata
- content-addressed artifact storage
- project-local `.pipe/` directory
- human-readable refs
- mount/publish projections
- provenance-aware runs

The tool is not source control. It is a **pipeline artifact and provenance manager**.
