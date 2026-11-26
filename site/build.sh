#!/usr/bin/env bash
# Builds the website into site/dist. The demo is the real internal/ui package, compiled for the browser.
set -euo pipefail
cd "$(dirname "$0")/.."

work=site/.build
out=site/dist
rm -rf "$work" "$out"
mkdir -p "$work" "$out"

# Bubble Tea and clipboard have no js build. Add the missing symbols to throwaway copies and point
# a separate modfile at them, so go.mod stays clean and "go install" keeps working.
cp go.mod "$work/go.mod"
cp go.sum "$work/go.sum"
patch_module() {
  local mod=$1 stub=$2 dir
  go mod download "$mod"
  dir=$(go list -m -f '{{.Dir}}' "$mod")
  cp -R "$dir" "$work/$stub"
  chmod -R u+w "$work/$stub"
  cp "site/wasm/stubs/$stub.go.txt" "$work/$stub/zz_js.go"
  go mod edit -replace "$mod=./$work/$stub" "$work/go.mod"
}
patch_module github.com/charmbracelet/bubbletea bubbletea
patch_module github.com/atotto/clipboard clipboard

GOOS=js GOARCH=wasm go build -modfile="$work/go.mod" -trimpath -ldflags "-s -w" -o "$work/sweetspot.wasm" ./site/wasm

cp -R site/src/. "$out/"
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$out/wasm_exec.js"
cp assets/logo.svg "$out/logo.svg"
# The page inflates this itself, so the download is small on every host.
gzip -9 -n -c "$work/sweetspot.wasm" > "$out/sweetspot.wasm.gz"
node site/poster.mjs "$out"
touch "$out/.nojekyll"
ls -l "$out" | awk '{print $5, $NF}'
