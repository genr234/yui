<script lang="ts">
  /**
   * While phones calibrate their motion, shows what to point at. Each
   * target is drawn once, with the players on it listed beside it in their
   * colours, so any number of phones can calibrate at once without targets
   * piling up:
   *  - scanning: the game goes black and one QR code fills its middle; the
   *    phone's camera reads it, and how it looks from the player's spot tells
   *    the phone their angle to the game;
   *  - aiming at the middle: a pulsing ring (a small dot on the code's centre
   *    while someone is still scanning);
   *  - aiming at the edges (phones without a camera): a glowing bar on the
   *    game's right edge, then on its top edge.
   * The controller lifts the kiosk's page above a running native game while
   * anyone is on one of these steps. Styles: styles/calibrate.css.
   */
  import qrcode from "qrcode-generator";
  import { onDestroy, onMount } from "svelte";
  import { bridge } from "../sdk/bridge";
  import { gameRect } from "../sdk/game-rect";
  import { pad, playerSwatch, type PadPlayer } from "../sdk/pad";
  import {
    aimArea,
    aimCode,
    applyCalibrate,
    boardFor,
    captionSpots,
    dropStale,
    targetFor,
    type Aiming,
  } from "../sdk/calibrate-targets";

  let aiming = new Map<string, Aiming>();
  let players: PadPlayer[] = [];
  let viewport = { w: window.innerWidth, h: window.innerHeight };
  const offs: Array<() => void> = [];
  let timer: number | undefined;

  onMount(() => {
    offs.push(bridge.on("pad.calibrate", (data) => (aiming = applyCalibrate(aiming, data, Date.now()))));
    offs.push(
      pad.onState((state) => {
        players = state.players;
        // A player who left (or the session ended) has nothing to aim at.
        const here = new Set(players.map((p) => p.id));
        const next = new Map([...aiming].filter(([id]) => here.has(id)));
        if (next.size !== aiming.size) aiming = next;
      }),
    );
    timer = window.setInterval(() => (aiming = dropStale(aiming, Date.now())), 5000);
  });
  onDestroy(() => {
    offs.forEach((off) => off());
    window.clearInterval(timer);
  });

  $: area = aimArea($gameRect, viewport);
  $: board = boardFor(aiming, players, playerSwatch);
  $: active = board.scan.length + board.center.length + board.right.length + board.top.length > 0;
  $: scanning = board.scan.length > 0;
  $: code = aimCode(area);
  $: spots = captionSpots(area, code);
  $: modules = scanning ? qrPath(code.text) : null;
  $: center = targetFor("aim-center", area);
  $: right = targetFor("aim-right", area);
  $: top = targetFor("aim-top", area);

  /** The code's dark modules as one SVG path, in module units. */
  function qrPath(text: string): { n: number; d: string } {
    const qr = qrcode(0, "H");
    qr.addData(text, "Alphanumeric");
    qr.make();
    const n = qr.getModuleCount();
    let d = "";
    for (let r = 0; r < n; r++) for (let c = 0; c < n; c++) if (qr.isDark(r, c)) d += `M${c} ${r}h1v1h-1z`;
    return { n, d };
  }
</script>

<svelte:window on:resize={() => (viewport = { w: window.innerWidth, h: window.innerHeight })} />

{#if active}
  <div class="aim-layer" aria-hidden="true">
    <div class="aim-area" class:scanning style="left:{area.x}px;top:{area.y}px;width:{area.w}px;height:{area.h}px"></div>

    {#if scanning && modules}
      <div
        class="aim-code"
        style="left:{code.x}px;top:{code.y}px;width:{code.w}px;height:{code.h}px;--margin:{0.14 * code.w}px"
      >
        <svg viewBox="0 0 {modules.n} {modules.n}" preserveAspectRatio="none" shape-rendering="crispEdges">
          <path d={modules.d} />
        </svg>
      </div>
      <div class="aim-caption" style="left:{spots.first.x}px;top:{spots.first.y}px">
        <span class="aim-caption-text">Scan this code with your phone</span>
        <span class="aim-chips">
          {#each board.scan as p (p.id)}<span class="aim-chip" style="--aim:{p.color}">{p.label}</span>{/each}
        </span>
      </div>
      {#if board.center.length}
        <span class="aim-center-dot" style="left:{center.x}px;top:{center.y}px"></span>
        <div class="aim-caption" style="left:{spots.second.x}px;top:{spots.second.y}px">
          <span class="aim-caption-text">Aim at the dot in the middle</span>
          <span class="aim-chips">
            {#each board.center as p (p.id)}<span class="aim-chip" style="--aim:{p.color}">{p.label}</span>{/each}
          </span>
        </div>
      {/if}
    {:else if board.center.length}
      <div class="aim-point" style="left:{center.x}px;top:{center.y}px">
        <span class="aim-ring"></span>
        <span class="aim-ring late"></span>
        <span class="aim-dot"></span>
        <span class="aim-caption under">
          <span class="aim-caption-text">Aim here</span>
          <span class="aim-chips">
            {#each board.center as p (p.id)}<span class="aim-chip" style="--aim:{p.color}">{p.label}</span>{/each}
          </span>
        </span>
      </div>
    {/if}

    {#if board.right.length && right.kind === "edge"}
      <div class="aim-edge vertical" style="left:{right.x}px;top:{right.y}px;height:{right.h}px"></div>
      <div class="aim-caption edge-right" style="left:{right.labelX}px;top:{right.labelY}px">
        <span class="aim-caption-text">Aim at this edge</span>
        <span class="aim-chips">
          {#each board.right as p (p.id)}<span class="aim-chip" style="--aim:{p.color}">{p.label}</span>{/each}
        </span>
      </div>
    {/if}
    {#if board.top.length && top.kind === "edge"}
      <div class="aim-edge" style="left:{top.x}px;top:{top.y}px;width:{top.w}px"></div>
      <div class="aim-caption edge-top" style="left:{top.labelX}px;top:{top.labelY}px">
        <span class="aim-caption-text">Aim at this edge</span>
        <span class="aim-chips">
          {#each board.top as p (p.id)}<span class="aim-chip" style="--aim:{p.color}">{p.label}</span>{/each}
        </span>
      </div>
    {/if}
  </div>
{/if}
