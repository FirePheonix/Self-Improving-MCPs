# Whitepaper

**Self-Improving MCP: Evidence-Gated Evolution of Tool Interfaces for AI Agents** — [`main.pdf`](main.pdf) (8 pages).

The system, the selection gate, the improver, the evaluation design, the results on five MCPToolBench++ domains, and the
measurement errors we found in our own earlier results.

## Build

Any TeX Live with `latexmk` (packages: natbib, algorithmicx, booktabs, microtype, lmodern):

```sh
cd paper && latexmk -pdf main.tex
```

Or with Docker, no local TeX needed:

```sh
docker run --rm -v "$PWD/paper:/paper" -w /paper texlive/texlive:latest-medium latexmk -pdf main.tex
```

Figures are vector PDFs printed from the light SVGs in `docs/assets/`. After changing a figure:

```sh
node docs/assets/build.mjs && sh paper/figures/build.sh
```
