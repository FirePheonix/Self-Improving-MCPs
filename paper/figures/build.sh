#!/bin/sh
# Regenerates the paper's vector figures from the light SVGs in docs/assets (run docs/assets/build.mjs first).
#   sh paper/figures/build.sh            (from the repository root; needs Google Chrome or Chromium)
# Each SVG is printed to a PDF page of exactly its own size, so the figure stays vector in the paper.
set -e
CHROME=${CHROME:-}
for c in "/c/Program Files/Google/Chrome/Application/chrome.exe" "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
         "$(command -v google-chrome 2>/dev/null)" "$(command -v chromium 2>/dev/null)"; do
  [ -z "$CHROME" ] && [ -n "$c" ] && [ -x "$c" ] && CHROME=$c
done
[ -n "$CHROME" ] || { echo "set CHROME to a Chrome/Chromium binary"; exit 1; }
abs() { if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else echo "$1"; fi; }
OUT=paper/figures
for name in how-it-works gate results ceiling models; do
  svg="docs/assets/$name-light.svg"
  vb=$(sed -n 's/.*viewBox="0 0 \([0-9.]*\) \([0-9.]*\)".*/\1 \2/p' "$svg" | head -1)
  w=${vb% *}; h=${vb#* }
  html="$OUT/.$name.html"
  printf '<html><head><style>@page{size:%spx %spx;margin:0}html,body{margin:0}img{display:block;width:%spx;height:%spx}</style></head><body><img src="file:///%s"></body></html>' \
    "$w" "$h" "$w" "$h" "$(abs "$(pwd)/$svg")" > "$html"
  "$CHROME" --headless=new --disable-gpu --no-pdf-header-footer --print-to-pdf="$(abs "$(pwd)/$OUT/$name.pdf")" "file:///$(abs "$(pwd)/$html")" >/dev/null 2>&1
  rm -f "$html"
  echo "wrote $OUT/$name.pdf (${w}x${h})"
done
