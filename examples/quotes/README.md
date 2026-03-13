# Quotes Example

This example turns a markdown file of Bible quote callouts into a printable PDF of quote cards.

It has three steps:

- `render`: converts `Quotes.md` into a LaTeX document
- `apply_overrides`: applies an optional patch set to the generated TeX
- `compile`: runs `pdflatex` and publishes the final PDF to `out/quotes.pdf`

Expected tools:

- `python3`
- `patch`
- `pdflatex`

From the repository root:

```bash
go build -o ./pakkun ./cmd/pakkun
cd examples/quotes
../../pakkun init
../../pakkun run quotes-build
../../pakkun show quotes-build:compile/pdf
../../pakkun publish quotes-build:compile/pdf ./build/quotes.pdf
```

This example is useful for understanding:

- script-driven pipeline stages that do more than simple shell one-liners
- publishing a final artifact automatically during `pakkun run`
- keeping generated `.tex`, `.log`, and `.pdf` files inside `.pipe/` until you explicitly publish them
