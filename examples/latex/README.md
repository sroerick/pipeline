# LaTeX Example

This example mirrors the original spec more closely, but it is only runnable if your machine has TeX tools installed.

Expected tools:

- `pdflatex`
- `bibtex`

From the repository root:

```bash
go build -o ./pipe ./cmd/pipe
cd examples/latex
../../pipe init
../../pipe run thesis
../../pipe show thesis:latex2/pdf
../../pipe publish thesis:latex2/pdf ./build/thesis.pdf
```

This example demonstrates:

- multiple outputs from a single stage
- a later stage consuming a prior artifact through `from`
- keeping `.aux`, `.log`, `.toc`, `.bbl`, and final `.pdf` artifacts under `.pipe/` instead of the working tree

If `pdflatex` or `bibtex` are missing, use `examples/text` first.
