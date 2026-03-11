# Text Example

This is the smallest runnable `pipe` project in the repository.

It has two steps:

- `copy`: copies `input.txt` into the managed step output directory
- `upper`: reads the stored `copy/text` artifact and writes an uppercased result

From the repository root:

```bash
go build -o ./pipe ./cmd/pipe
cd examples/text
../../pipe init
../../pipe run text-demo
../../pipe show text-demo:upper/result
../../pipe publish text-demo:upper/result ./build/result.txt
cat ./build/result.txt
```

Expected final content:

```text
HELLO PIPELINE
```

Good inspection commands:

```bash
../../pipe stages
../../pipe log
../../pipe show text-demo:copy
../../pipe provenance text-demo:upper/result
```
