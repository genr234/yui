#!/usr/bin/env bash
# Repackage the official Cemu Windows zip into the flat runtime zip Wii U Arcade expects:
# Cemu.exe at the root, no versioned top folder, an empty portable/ folder beside it.
# Usage: tools/build-runtime.sh <cemu-2.6-windows-x64.zip> [output.zip]
# Nothing is downloaded: pass the archive you obtained from the Cemu project.
set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
	echo "usage: $0 <cemu-windows-x64.zip> [output.zip]" >&2
	exit 2
fi

input=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
out=${2:-cemu-2.6.zip}
case "$out" in /*) ;; *) out="$(pwd)/$out" ;; esac

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

unzip -q "$input" -d "$work/extracted"

# Official archives hold one versioned folder (Cemu_x.y.z); descend into it. Otherwise use the root.
root="$work/extracted"
entries=$(find "$root" -mindepth 1 -maxdepth 1 | wc -l | tr -d ' ')
if [ "$entries" = "1" ] && [ -d "$(find "$root" -mindepth 1 -maxdepth 1)" ]; then
	root=$(find "$root" -mindepth 1 -maxdepth 1)
fi

if [ ! -f "$root/Cemu.exe" ]; then
	echo "Cemu.exe not found in $input" >&2
	exit 1
fi

mkdir -p "$root/portable"
rm -f "$out"
(cd "$root" && zip -qr "$out" .)
echo "wrote $out"
