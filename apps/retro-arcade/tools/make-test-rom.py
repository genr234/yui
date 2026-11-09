#!/usr/bin/env python3
"""Builds yui-input-test.nes: a tiny NES program written for Retro Arcade's
tests (no third-party code or data). The whole screen shows one colour:
  port 1 A (East) -> red, port 1 B (South) -> green,
  other port 1 buttons -> a colour from the button bits,
  any port 2 button -> yellow, nothing pressed -> blue.
Usage: python3 make-test-rom.py out.nes
"""
import sys

ORG = 0xC000
code = bytearray()
labels, fixups = {}, []  # fixups: (pos, label, kind) kind = "abs" | "rel"

def emit(*bs): code.extend(bs)
def label(name): labels[name] = ORG + len(code)
def abs_(op, name): emit(op, 0, 0); fixups.append((len(code) - 2, name, "abs"))
def rel(op, name): emit(op, 0); fixups.append((len(code) - 1, name, "rel"))
def sta(addr): emit(0x8D, addr & 0xFF, addr >> 8)
def lda_abs(addr): emit(0xAD, addr & 0xFF, addr >> 8)

label("reset")
emit(0x78, 0xD8, 0xA2, 0x40); emit(0x8E, 0x17, 0x40)      # SEI CLD LDX #$40 STX $4017
emit(0xA2, 0xFF, 0x9A, 0xE8)                               # LDX #$FF TXS INX
emit(0x8E, 0x00, 0x20, 0x8E, 0x01, 0x20, 0x8E, 0x10, 0x40) # STX $2000/$2001/$4010
for name in ("vw1", "vw2"):
    label(name); emit(0x2C, 0x02, 0x20); rel(0x10, name)   # BIT $2002 BPL
label("main")
emit(0xA9, 0x01); sta(0x4016); emit(0xA9, 0x00); sta(0x4016)
emit(0x85, 0x00, 0x85, 0x01, 0xA2, 0x08)                   # STA $00 STA $01 LDX #8
label("read")
lda_abs(0x4016); emit(0x4A, 0x26, 0x00)                     # LSR ROL $00
lda_abs(0x4017); emit(0x4A, 0x26, 0x01)                     # LSR ROL $01
emit(0xCA); rel(0xD0, "read")                              # DEX BNE
label("vw3"); emit(0x2C, 0x02, 0x20); rel(0x10, "vw3")
emit(0xA9, 0x00); sta(0x2001)                              # rendering off while writing
emit(0xA9, 0x3F); sta(0x2006); emit(0xA9, 0x00); sta(0x2006)
emit(0xA5, 0x00); rel(0x10, "notA")                        # LDA $00 BPL notA (A is bit 7)
emit(0xA9, 0x16); abs_(0x4C, "write")                      # red
label("notA"); emit(0x29, 0x40); rel(0xF0, "notB")         # AND #$40 BEQ
emit(0xA9, 0x2A); abs_(0x4C, "write")                      # green
label("notB"); emit(0xA5, 0x00, 0x29, 0x3F); rel(0xF0, "p2")
abs_(0x4C, "write")
label("p2"); emit(0xA5, 0x01); rel(0xF0, "idle")
emit(0xA9, 0x28); abs_(0x4C, "write")                      # yellow
label("idle"); emit(0xA9, 0x11)                            # blue
label("write"); sta(0x2007)
emit(0xA9, 0x00); sta(0x2006); sta(0x2006); sta(0x2005); sta(0x2005)
emit(0xA9, 0x08); sta(0x2001)                              # background on
abs_(0x4C, "main")
label("nmi"); emit(0x40)                                   # RTI

for pos, name, kind in fixups:
    target = labels[name]
    if kind == "abs":
        code[pos] = target & 0xFF; code[pos + 1] = target >> 8
    else:
        offset = target - (ORG + pos + 1)
        assert -128 <= offset <= 127, name
        code[pos] = offset & 0xFF

prg = bytearray(16384)
prg[: len(code)] = code
for vec, name in ((0x3FFA, "nmi"), (0x3FFC, "reset"), (0x3FFE, "nmi")):
    prg[vec] = labels[name] & 0xFF; prg[vec + 1] = labels[name] >> 8
header = b"NES\x1a" + bytes([1, 1, 0, 0]) + bytes(8)
open(sys.argv[1], "wb").write(header + prg + bytes(8192))
print(f"wrote {sys.argv[1]} ({len(code)} bytes of code)")
