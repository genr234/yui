# Kenney Mobile Controls icons

These SVG glyphs come from Kenney's "Mobile Controls" pack (https://kenney.nl/assets/mobile-controls),
released under Creative Commons Zero (CC0 1.0). No attribution is required; credit: Kenney (www.kenney.nl).
Only the icons the pad uses are copied here. See `pad/src/Icon.svelte`.

Emote art in assets/emotes is also Kenney (Emotes Pack, CC0): https://kenney.nl/assets/emotes-pack

Control art in assets/kenney/controls is also from Mobile Controls (CC0, credit Kenney, www.kenney.nl):
style F (flat light) buttons, d-pad and joystick, the orange glow outlines (`hl_*`, highlights A) and the
A/B/X/Y/L/R button glyphs (`glyph_*`). Used by `src/controls/art.ts`, which inlines the SVGs and swaps their
fills for CSS classes so the pad can recolour them in the player's colour.
