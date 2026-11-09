#!/usr/bin/env bash
# Builds the `emulatorjs` runtime asset for Retro Arcade from the official
# EmulatorJS release archive. Upload the zip in the control room as a
# runtime asset with metadata runtime=emulatorjs, version=4.2.3.
#
#   tools/build-runtime.sh 4.2.3.7z emulatorjs-4.2.3.zip [cores...]
#
# Default cores are the ones Retro Arcade uses (see LICENSES.md; some are
# non-commercial only, so the owner decides which to ship).
set -euo pipefail
archive=${1:?usage: build-runtime.sh <EmulatorJS .7z> <out.zip> [cores...]}
out=${2:?usage: build-runtime.sh <EmulatorJS .7z> <out.zip> [cores...]}
shift 2
case "$out" in /*) ;; *) out="$PWD/$out" ;; esac
cores=("$@")
if [ ${#cores[@]} -eq 0 ]; then
  cores=(fceumm snes9x gambatte mgba genesis_plus_gx smsplus mednafen_pce stella2014 handy)
fi
expected=07d451bc06fa3ad04ab30d9b94eb63ac34ad0babee52d60357b002bde8f3850b
actual=$(shasum -a 256 "$archive" | cut -d' ' -f1)
if [ "$actual" != "$expected" ]; then
  echo "unexpected archive (sha256 $actual); Retro Arcade is pinned to EmulatorJS 4.2.3" >&2
  exit 1
fi
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
tar -xf "$archive" -C "$work"
data="$work/data"
stage="$work/runtime"
mkdir -p "$stage/cores/reports"
cp -R "$data/emulator.min.js" "$data/emulator.min.css" "$data/loader.js" "$data/version.json" "$data/compression" "$data/src" "$stage/"
cp "$data/cores/cores.json" "$stage/cores/"
cp "$work/LICENSE" "$stage/LICENSE-EmulatorJS"
for core in "${cores[@]}"; do
  cp "$data/cores/$core-wasm.data" "$data/cores/$core-legacy-wasm.data" "$stage/cores/"
  [ -f "$data/cores/reports/$core.json" ] && cp "$data/cores/reports/$core.json" "$stage/cores/reports/"
done
rm -f "$out"
(cd "$stage" && zip -qr -X "$out" .)
echo "wrote $out ($(du -h "$out" | cut -f1)) with cores: ${cores[*]}"
