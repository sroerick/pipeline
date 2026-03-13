# LaTeX Example

This example mirrors the original spec more closely, but it is only runnable if your machine has TeX tools installed.

Expected tools:

- `pdflatex`
- `bibtex`

From the repository root:

```bash
go build -o ./pakkun ./cmd/pakkun
cd examples/latex
../../pakkun init
../../pakkun run thesis
../../pakkun show thesis:latex2/pdf
../../pakkun publish thesis:latex2/pdf ./build/thesis.pdf
```

This example demonstrates:

- multiple outputs from a single stage
- a later stage consuming a prior artifact through `from`
- keeping `.aux`, `.log`, `.toc`, `.bbl`, and final `.pdf` artifacts under `.pipe/` instead of the working tree

If `pdflatex` or `bibtex` are missing, use `examples/text` first.
