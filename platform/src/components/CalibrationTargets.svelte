<script lang="ts">
  /**
   * While a phone calibrates its motion, shows that player what to point at.
   * Scanning: the game's area goes black with four marker squares for the
   * phone's camera to find (how they look from the player's spot tells the
   * phone its angle to the game). Then a pulsing crosshair at the middle of
   * the game; and, when a phone aims at the edges instead, a glowing bar on
   * the game's right edge, then on its top edge. Styles: styles/calibrate.css.
   */
  import { onDestroy, onMount } from "svelte";
  import { bridge } from "../sdk/bridge";
  import { gameRect } from "../sdk/game-rect";
  import { pad, playerSwatch, type PadPlayer } from "../sdk/pad";
  import { aimArea, applyCalibrate, dropStale, scanMarkers, targetFor, type Aiming } from "../sdk/calibrate-targets";

  let aiming = new Map<string, Aiming>();
  let players: PadPlayer[] = [];
  let viewport = { w: window.innerWidth, h: window.innerHeight };
  const offs: Array<() => void> = [];
  let timer: number | undefined;

  onMount(() => {
    offs.push(bridge.on("pad.calibrate", (data) => (aiming = applyCalibrate(aiming, data, Date.now()))));
    offs.push(pad.onState((state) => {
      players = state.players;
      // A player who left (or the session ended) has nothing to aim at.
      const here = new Set(players.map((p) => p.id));
      const next = new Map([...aiming].filter(([id]) => here.has(id)));
      if (next.size !== aiming.size) aiming = next;
    }));
    timer = window.setInterval(() => (aiming = dropStale(aiming, Date.now())), 5000);
  });
  onDestroy(() => {
    offs.forEach((off) => off());
    window.clearInterval(timer);
  });

  $: area = aimArea($gameRect, viewport);
  // One set of markers serves every phone scanning at once.
  $: scanning = shown.some((s) => s.step === "scan");
  $: markers = scanning ? scanMarkers(area) : [];
  $: shown = [...aiming.values()].map((a, i) => {
    const player = players.find((p) => p.id === a.playerId);
    return {
      key: a.playerId,
      step: a.step,
      target: targetFor(a.step, area),
      color: (player && playerSwatch[player.color]) || "#2f76dc",
      label: player?.slot ? `P${player.slot}` : player?.name || "Player",
      // Several players aiming at once: stack their labels.
      nudge: i * 34,
    };
  });
</script>

<svelte:window on:resize={() => (viewport = { w: window.innerWidth, h: window.innerHeight })} />

{#if shown.length}
  <div class="aim-layer" aria-hidden="true">
    <div class="aim-area" class:scanning style="left:{area.x}px;top:{area.y}px;width:{area.w}px;height:{area.h}px"></div>
    {#each markers as m}
      <div class="scan-marker" style="left:{m.x}px;top:{m.y}px;width:{m.size}px;height:{m.size}px"></div>
    {/each}
    {#each shown as s (s.key + s.step)}
      {#if s.target.kind === "scan"}
        <div class="aim-label scan-label" style="left:{s.target.x}px;top:{s.target.y + s.nudge}px;--aim:{s.color}">
          {s.label} · scan this with your phone
        </div>
      {:else if s.target.kind === "point"}
        <div class="aim-point" style="left:{s.target.x}px;top:{s.target.y}px;--aim:{s.color}">
          <span class="aim-ring"></span>
          <span class="aim-ring late"></span>
          <span class="aim-dot"></span>
          <span class="aim-label" style="margin-top:{s.nudge}px">{s.label} · aim here</span>
        </div>
      {:else}
        <div
          class="aim-edge"
          class:vertical={s.step === "aim-right"}
          style="left:{s.target.x}px;top:{s.target.y}px;width:{s.target.w}px;height:{s.target.h}px;--aim:{s.color}"
        ></div>
        <div
          class="aim-label edge-label"
          class:right={s.step === "aim-right"}
          style="left:{s.target.labelX}px;top:{s.target.labelY + s.nudge}px;--aim:{s.color}"
        >
          {s.label} · aim at this edge
        </div>
      {/if}
    {/each}
  </div>
{/if}
