#!/usr/bin/env bash
# Rebuilds libopus.wasm: libopus compiled to a WASI reactor module that the
# controller runs under wazero, so the controller stays cgo-free.
#
# Requirements: zig (pinned below; it ships clang + wasi-libc), curl, tar,
# shasum. Run from anywhere:
#
#   controller/internal/audio/opus/wasm/build.sh
#
# The output is deterministic for a given zig version and libopus tarball.
set -euo pipefail

OPUS_VERSION=1.5.2
OPUS_SHA256=65c1d2f78b9f2fb20082c38cbe47c951ad5839345876e46941612ee87f9a7ce1
ZIG_VERSION=0.15.2

here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if [ "$(zig version)" != "$ZIG_VERSION" ]; then
  echo "warning: zig $(zig version) differs from pinned $ZIG_VERSION; output may not be byte-identical" >&2
fi

tarball="opus-$OPUS_VERSION.tar.gz"
curl -sSfL -o "$work/$tarball" "https://downloads.xiph.org/releases/opus/$tarball"
echo "$OPUS_SHA256  $work/$tarball" | shasum -a 256 -c -
tar -xzf "$work/$tarball" -C "$work"
src="$work/opus-$OPUS_VERSION"

# Read a source list from libopus's own .mk files.
mk_list() { # file var
  awk -v var="$2" '
    $1 == var && $2 == "=" { on = 1; next }
    on {
      line = $0; cont = (line ~ /\\[ \t]*$/); gsub(/\\/, "", line)
      n = split(line, a); for (i = 1; i <= n; i++) print a[i]
      if (!cont) on = 0
    }
  ' "$src/$1"
}

sources=()
for pair in "opus_sources.mk OPUS_SOURCES" "opus_sources.mk OPUS_SOURCES_FLOAT" \
            "celt_sources.mk CELT_SOURCES" "silk_sources.mk SILK_SOURCES" \
            "silk_sources.mk SILK_SOURCES_FLOAT"; do
  # shellcheck disable=SC2086
  while IFS= read -r f; do
    [ -n "$f" ] && sources+=("$src/$f")
  done < <(mk_list $pair)
done

# Float build (the default libopus configuration). VAR_ARRAYS puts scratch
# buffers on the wasm stack, so the stack is sized generously.
zig cc -target wasm32-wasi -mexec-model=reactor -O2 -DNDEBUG \
  -DOPUS_BUILD -DPACKAGE_VERSION="\"$OPUS_VERSION\"" -DVAR_ARRAYS -DHAVE_LRINT -DHAVE_LRINTF -DFLOAT_APPROX \
  -I"$src/include" -I"$src/celt" -I"$src/silk" -I"$src/silk/float" -I"$src/src" \
  -ffunction-sections -fdata-sections \
  -Wl,--gc-sections -Wl,--strip-all -Wl,-z,stack-size=1048576 \
  -Wl,--export=malloc -Wl,--export=free \
  -Wl,--export=opus_encoder_create -Wl,--export=opus_encoder_destroy \
  -Wl,--export=opus_encode -Wl,--export=opus_encode_float \
  -Wl,--export=opus_decoder_create -Wl,--export=opus_decoder_destroy \
  -Wl,--export=opus_decode -Wl,--export=opus_decode_float \
  -Wl,--export=opus_get_version_string \
  -o "$here/libopus.wasm" \
  "$here/bridge.c" "${sources[@]}"

cp "$src/COPYING" "$here/COPYING.libopus"
shasum -a 256 "$here/libopus.wasm"
