<script lang="ts">
  /**
   * Bottom bar: one chunky chip per seated player (colour, avatar, name,
   * Yui slot, dots that light while buttons are held), a "+N watching" pill,
   * and the rising reaction balloons and poke wiggles that belong to them.
   * Chips are labelled by Yui slot (P1..P4): the overlay is not told which
   * controller a phone drives (virtual-pad attachments stay inside the controller).
   */
  import { onDestroy, onMount } from "svelte";
  import HandIcon from "lucide-svelte/icons/hand";
  import type { Rect } from "../../sdk/game-rect";
  import { avatarKind, type PadPlayer, type PadState } from "../../sdk/pad";
  import { emoteUrl } from "./emotes";
  import { chipLayout, JOIN_RESERVE_PX, joinVisible, seated, spectators, swatchOf } from "./chips";
  import { BUTTON_DOTS, dotsLit, social } from "./social";
  import { watchPadState } from "./padSession";

  // svelte-ignore unused-export-let
  export let rect: Rect | undefined = undefined;
  // svelte-ignore unused-export-let
  export let gameRect: Rect | undefined = undefined;
  export let players: PadPlayer[] = [];
  // svelte-ignore unused-export-let
  export let bar: "top" | "bottom" = "bottom";

  let session: PadState["session"] = null;
  const offs: Array<() => void> = [];
  onMount(() => {
    offs.push(social.start());
    if (import.meta.env.VITE_YUI_DEMO === "true") void import("./social-demo");
    offs.push(watchPadState((state) => (session = state.session)));
  });
  onDestroy(() => offs.forEach((off) => off()));

  $: seats = seated(players);
  $: watchers = spectators(players);
  $: seatIds = new Set(seats.map((p) => p.id));
  $: joinShown = joinVisible(session, players);
  $: layout = chipLayout(seats.length, rect?.w ?? 540, rect?.h ?? 280, joinShown ? JOIN_RESERVE_PX : 0);
  $: unit = layout.h / 84;

  /** Spread balloons sideways so a burst does not stack exactly. */
  function drift(key: number): number {
    return ((key * 37) % 5 - 2) * 12;
  }

  $: pokedBy = new Map($social.pokes.map((p) => [p.to, p]));
  $: spectatorBubbles = $social.bubbles.filter((b) => !seatIds.has(b.playerId) && watchers.some((w) => w.id === b.playerId));
  function colorOf(id: string) {
    return swatchOf(players.find((p) => p.id === id)?.color);
  }
</script>

<div class="seats" style="--unit:{unit}">
  {#if watchers.length > 0}
    <div class="seat-watch-row">
      <div class="watch-pill" title={watchers.map((p) => p.name).join(", ")}>
        +{watchers.length} watching
        <div class="bubble-layer">
          {#each spectatorBubbles as bubble (bubble.key)}
            <img class="bubble" src={emoteUrl(bubble.emote)} alt="" draggable="false" style="--dx:{drift(bubble.key)}px" />
          {/each}
        </div>
      </div>
    </div>
  {/if}

  {#if seats.length > 0}
    <div
      class="seat-grid"
      class:with-join={joinShown}
      style="grid-template-columns:repeat({layout.cols}, {layout.w}px);gap:{layout.gap}px;--chip-h:{layout.h}px"
    >
      {#each seats as player (player.id)}
        {@const avatar = avatarKind(player.avatar)}
        {@const poke = pokedBy.get(player.id)}
        {@const lit = dotsLit($social.held[player.id] ?? 0)}
        <div class="seat">
          <div class="bubble-layer">
            {#each $social.bubbles.filter((b) => b.playerId === player.id) as bubble (bubble.key)}
              <img class="bubble" src={emoteUrl(bubble.emote)} alt="" draggable="false" style="--dx:{drift(bubble.key)}px" />
            {/each}
          </div>
          {#key poke?.key ?? 0}
            <div
              class="seat-chip"
              class:away={!player.connected}
              class:poked={Boolean(poke)}
              style="--seat:{swatchOf(player.color)}"
              data-player={player.id}
            >
              <span class="seat-avatar" class:has-avatar={avatar}>
                {#if avatar?.kind === "image"}
                  <img src={avatar.src} alt="" draggable="false" />
                {:else if avatar}
                  <span class="emoji">{avatar.text}</span>
                {:else}
                  P{player.slot}
                {/if}
              </span>
              <span class="seat-text">
                <strong>{player.name}</strong>
                <span>{player.connected ? `P${player.slot}${player.host ? " · host" : ""}` : "Reconnecting…"}</span>
              </span>
              <span class="seat-dots" aria-hidden="true">
                {#each BUTTON_DOTS as dot, i (dot.id)}
                  <i class="seat-dot dot-{dot.id}" class:on={player.connected && lit[i]}></i>
                {/each}
              </span>
              {#if poke}
                <span class="seat-poke" style="color:{colorOf(poke.from)}" aria-hidden="true">
                  <HandIcon size={28} strokeWidth={2.6} />
                </span>
              {/if}
            </div>
          {/key}
        </div>
      {/each}
    </div>
  {/if}
</div>
