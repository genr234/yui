<script lang="ts">
	import { onDestroy, onMount } from "svelte";
	import { pad, playerSwatch, type PadState } from "../../sdk/pad";
	import JoinSteps from "../JoinSteps.svelte";

	/** QR size in px. */
	export let size = 132;
	/** Show the "Play on your phones" button when no session is running. */
	export let canStart = true;

	let state: PadState = { session: null, players: [] };
	let off: (() => void) | undefined;

	onMount(() => {
		void pad.state().then((next) => (state = next));
		off = pad.onState((next) => (state = next));
	});
	onDestroy(() => off?.());
</script>

<!-- ctx.controllers.joinCard(): the live join code and QR. The join URL
     (QR token) is drawn here and never handed to the app. -->
<div class="pad-join">
	{#if state.session}
		<JoinSteps session={state.session} {size} />
		<div class="pad-join-text">
			<span>{state.session.lobby_open ? "Or enter the code" : "Session code"}</span>
			<strong>{state.session.code}</strong>
			{#if state.players.length}
				<ul class="pad-join-players">
					{#each state.players as player (player.id)}
						<li style={`--swatch: ${playerSwatch[player.color] ?? "var(--yui-blue)"}`}>
							{player.name || "Player"}{player.host ? " (host)" : ""}
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	{:else}
		<div class="pad-join-text">
			<span>Play with your phones</span>
			{#if canStart}
				<button class="yui-app-button primary" on:click={() => pad.run("start")}>Start phone play</button>
			{/if}
		</div>
	{/if}
</div>

