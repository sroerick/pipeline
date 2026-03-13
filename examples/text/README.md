# Text Example

This is the smallest runnable `pakkun` project in the repository.

It has two steps:

- `copy`: copies `input.txt` into the managed step output directory
- `upper`: reads the stored `copy/text` artifact and writes an uppercased result

From the repository root:

```bash
go build -o ./pakkun ./cmd/pakkun
cd examples/text
../../pakkun init
../../pakkun run text-demo
../../pakkun show text-demo:upper/result
../../pakkun publish text-demo:upper/result ./build/result.txt
cat ./build/result.txt
```

Expected final content:

```text
HELLO PIPELINE
```

Good inspection commands:

```bash
../../pakkun stages
../../pakkun log
../../pakkun show text-demo:copy
../../pakkun provenance text-demo:upper/result
```
