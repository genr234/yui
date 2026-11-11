<script lang="ts">
  /**
   * Bottom bar, right corner: a small join QR with "Join or watch" and how
   * many are watching. Shown while the session's lobby is open or a seat is
   * free (see `joinVisible`); hidden for a full table with the lobby closed.
   * The URL rotates every 30 s, so it always comes from the live session.
   */
  import { onDestroy, onMount } from "svelte";
  import type { Rect } from "../../sdk/game-rect";
  import type { PadPlayer, PadState } from "../../sdk/pad";
  import JoinQr from "../JoinQr.svelte";
  import { joinVisible, spectators } from "./chips";
  import { watchPadState } from "./padSession";

  // svelte-ignore unused-export-let
  export let rect: Rect | undefined = undefined;
  // svelte-ignore unused-export-let
  export let gameRect: Rect | undefined = undefined;
  export let players: PadPlayer[] = [];
  // svelte-ignore unused-export-let
  export let bar: "top" | "bottom" = "bottom";

  let session: PadState["session"] = null;
  let off: (() => void) | undefined;
  onMount(() => (off = watchPadState((state) => (session = state.session))));
  onDestroy(() => off?.());

  $: show = joinVisible(session, players);
  $: watching = spectators(players).length;
  $: size = Math.max(84, Math.min(116, (rect?.h ?? 280) * 0.4));
</script>

{#if show && session}
  <div class="join-corner">
    <div class="join-qr-frame">
      <JoinQr value={session.join_url} {size} quiet={2} label="QR code to join or watch" />
    </div>
    <strong>Join or watch</strong>
    <span>{watching} watching</span>
  </div>
{/if}
