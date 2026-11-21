# Deltarune Boss Practice

In the Yui control room, open **Asset Library** and upload
`deltarune-boss-practice-8.zip` once. Choose **Runtime** for the asset type,
enter `roaring-knight` in the **Runtime** field, and set **Version** to `8`.
Let the kiosk sync the runtime before going offline.

Open **Deltarune Boss Practice** from Yui and choose a fight. Chapter 3
loads the bundled checkpoint automatically. When the fight ends, use
**Switch fight** to go back to the selection screen and start another round.

Choose **Enable head control**, then face the kiosk camera and hold still
until calibration completes. In the practice box, try the three sensitivity
settings. Lean in the direction you want the heart to move and return to
center to stop. Face tracking runs on the kiosk and the camera preview
appears during calibration.

If the camera loses your face for about half a second, the fight pauses.
Find the camera again and use **Resume fight**. If your position changed,
use **Recenter** first. The phone pad's **Recenter / Resume** button does
both in sequence when needed. **Turn off camera** resumes keyboard play.

The host's Yui pad handles battle menus and actions. The kiosk keyboard's
arrow keys are also available for heart movement. Boxing and ERAM use their
own movement controls. Head steering requires app `0.7.0`, runtime `8`,
and a Yui platform build with the camera face-tracking API.

| Chapter | Fights |
| --- | --- |
| 1 | King, Jevil |
| 2 | Queen, Giga Queen, Spamton, Spamton NEO |
| 3 | Tenna, Roaring Knight, ERAM |
| 4 | Jackenstein, Sound of Justice, Titan, Hammer of Justice (Gerson) |

For the local browser demo, extract the ZIP into
`platform/.demo-runtimes/roaring-knight/`. Run Yui in demo mode and open the
same app from its catalog.

## Runtime build

The archive contains the patched Chapter 3 at its root, with its
`filech3_0` checkpoint. Chapters 1, 2 and 4 have separate folders with
chapter-specific data and HTML launchers. All four share the root
`runner.wasm`. Keep the launchers' scripts and assets local to the archive
and run loader code from external scripts for Yui's content security policy.

Join split `game.unx` files from the supplied export in numeric order
before patching with UndertaleModCli 0.9.2.0. Use
[ChapterOne.csx](ChapterOne.csx) for Chapter 1,
[DirectBattle.csx](DirectBattle.csx) for Chapter 3, and
[ChapterTwoAndFour.csx](ChapterTwoAndFour.csx) with `YUI_BOSS_CHAPTER` set
to `2` or `4` for the other chapters.
Apply [MenuPad.csx](MenuPad.csx) and [HeartSteering.csx](HeartSteering.csx)
to each patched chapter before splitting. Put
[head-input.js](runtime/head-input.js) at the archive root and load it at
the end of each chapter's HTML, following the platform's `_yui/embed.js`
helper. Use paths relative to the chapter.

```sh
UndertaleModCli load original.unx -s DirectBattle.csx -o direct.unx
```

Split the patched data to match the chapter loader. Use these byte counts
for all parts before the final part, then write the remainder as the final part.

| Chapter | Number of parts | Bytes per part before the final part |
| --- | ---: | ---: |
| 1 | 1 | |
| 2 | 4 | 16,737,286 |
| 3 | 8 | 15,777,487 |
| 4 | 8 | 16,501,512 |
