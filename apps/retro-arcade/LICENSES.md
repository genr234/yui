# Licences

Retro Arcade's own code is part of Yui. The emulator is a separate runtime asset the owner uploads; nothing below is committed to this repository.

| Part | Licence | Commercial use |
|---|---|---|
| EmulatorJS 4.2.3 | GPL-3.0 | yes |
| fceumm (NES) | GPL-2.0 | yes |
| gambatte (Game Boy / Color) | GPL-2.0 | yes |
| mgba (Game Boy Advance) | MPL-2.0 | yes |
| mednafen_pce (PC Engine) | GPL-2.0 | yes |
| stella2014 (Atari 2600) | GPL-2.0 | yes |
| handy (Lynx) | zlib | yes |
| **snes9x** (Super NES) | Snes9x licence | **non-commercial only** |
| **genesis_plus_gx** (Mega Drive, Game Gear, Master System) | Genesis Plus GX licence | **non-commercial only** |
| **smsplus** (Master System) | GPL-2.0 (SMS Plus GX) | yes; check before shipping |

**The owner decides.** `tools/build-runtime.sh` includes the non-commercial cores by default, because Yui isn't sold. Pass an explicit core list to leave them out. Without snes9x and genesis_plus_gx, Super NES, Mega Drive and Game Gear games show "no core" errors.

Fonts: Press Start 2P (SIL Open Font Licence 1.1), in `web/fonts/`.

Games: only files the owner has the right to use, uploaded or imported (for example from their own RomM library). This repository contains no game ROMs except `test/roms/yui-input-test.nes`, which Yui generated (`tools/make-test-rom.py`).
