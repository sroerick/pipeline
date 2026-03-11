# Compiler Example

This example is shaped like a toy compiler pipeline.

It has three steps:

- `parse`: tokenizes `source.expr`
- `typecheck`: prefixes each token with `typed:`
- `codegen`: converts the typed form into an emitted program listing

From the repository root:

```bash
go build -o ./pipe ./cmd/pipe
cd examples/compiler
../../pipe init
../../pipe run compiler-demo
../../pipe show compiler-demo:codegen/program
../../pipe publish compiler-demo:codegen/program ./build/program.txt
cat ./build/program.txt
```

Useful stage refs:

- `compiler-demo:parse/tokens`
- `compiler-demo:typecheck/typed_ast`
- `compiler-demo:codegen/program`

This example is useful for understanding how `from: <step>/<output>` turns into `PIPE_INPUT_<output-name>` inside later steps.
