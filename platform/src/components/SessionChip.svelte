<script lang="ts">
	import { onMount } from "svelte";
	import SmartphoneIcon from "lucide-svelte/icons/smartphone";
	import CrownIcon from "lucide-svelte/icons/crown";
	import Volume2Icon from "lucide-svelte/icons/volume-2";
	import XIcon from "lucide-svelte/icons/x";
	import JoinQr from "./JoinQr.svelte";
	import { avatarKind, linkQuality, pad, padAction, playerSwatch, type PadPlayer, type PadState } from "../sdk/pad";

	/** Session and roster, owned by the top bar. */
	export let padState: PadState = { session: null, players: [] };
	/** Player ids that just connected (the squares pop in). */
	export let arrived: Record<string, boolean> = {};

	const slots = [1, 2, 3, 4];

	let open = false;
	let wrap: HTMLDivElement | undefined;
	let confirmEnd = false;
	let confirmKick = "";
	let lobbyBusy = false;

	$: session = padState.session;
	$: playerBySlot = new Map(padState.players.filter((player) => player.slot).map((player) => [player.slot, player]));
	$: seated = padState.players.filter((player) => player.slot);
	$: hasSpeaker = padState.players.some((player) => player.speaker);
	$: watching = padState.players.filter((player) => !player.slot);
	$: if (!session) {
		open = false;
		confirmEnd = false;
		confirmKick = "";
	}
	$: joinHost = session ? hostOf(session.join_url) : "";

	onMount(() => {
		const onPointerDown = (event: PointerEvent) => {
			if (!open || !wrap) return;
			if (event.composedPath().includes(wrap)) return;
			close();
		};
		const onKeyDown = (event: KeyboardEvent) => {
			if (!open || event.key !== "Escape") return;
			event.stopPropagation();
			close();
		};
		window.addEventListener("pointerdown", onPointerDown);
		window.addEventListener("keydown", onKeyDown);
		return () => {
			window.removeEventListener("pointerdown", onPointerDown);
			window.removeEventListener("keydown", onKeyDown);
		};
	});

	function close() {
		open = false;
		confirmEnd = false;
		confirmKick = "";
	}

	async function start() {
		const next = await pad.run("start");
		if (next?.session) open = true;
	}

	async function end() {
		if (!confirmEnd) {
			confirmEnd = true;
			return;
		}
		const next = await pad.run("end");
		if (next) close();
	}

	async function toggleLobby() {
		if (!session || lobbyBusy) return;
		lobbyBusy = true;
		await pad.setLobby(!session.lobby_open);
		lobbyBusy = false;
	}

	async function kick(player: PadPlayer) {
		if (confirmKick !== player.id) {
			confirmKick = player.id;
			return;
		}
		confirmKick = "";
		await pad.kick(player.id);
	}

	function hostOf(url: string) {
		try {
			return new URL(url).host;
		} catch {
			return "";
		}
	}

	function swatch(player: PadPlayer) {
		return playerSwatch[player.color] ?? "#2f76dc";
	}

	/** " (left)" / " (right)" for a phone in a stereo pair. */
	function channelTag(player: PadPlayer) {
		return player.speaker_channel ? ` (${player.speaker_channel})` : "";
	}

	/** "L" / "R" for a phone in a stereo pair, else "". */
	function channelLetter(player: PadPlayer) {
		return player.speaker_channel === "left" ? " L" : player.speaker_channel === "right" ? " R" : "";
	}

	function squareTitle(slot: number) {
		const player = playerBySlot.get(slot);
		if (!player) return `Player ${slot}: open`;
		const roles = [player.host && "host", player.speaker && `speaker${channelTag(player)}`].filter(Boolean).join(", ");
		return `Player ${slot}: ${player.name}${roles ? ` (${roles})` : ""}`;
	}
</script>

<div class="session-wrap" bind:this={wrap}>
	{#if session}
		<button
			class="topbar-chip session-chip"
			type="button"
			aria-expanded={open}
			aria-haspopup="dialog"
			aria-label={`Phone play: code ${session.code}, ${seated.length} of 4 players`}
			on:click={() => (open ? close() : (open = true))}
		>
			<span class="session-code">{session.code}</span>
			<span class="session-squares" aria-hidden="true">
				{#each slots as slot}
					{@const player = playerBySlot.get(slot)}
					{@const avatar = player ? avatarKind(player.avatar) : null}
					<span
						class="player-square"
						class:open={!player}
						class:away={player && !player.connected}
						class:arrive={player && arrived[player.id]}
						class:has-avatar={avatar}
						title={squareTitle(slot)}
						style={player ? `background: ${swatch(player)}` : ""}
					>
						{#if avatar?.kind === "image"}
							<img class="player-avatar" src={avatar.src} alt="" draggable="false" />
						{:else if avatar}
							<span class="player-avatar emoji">{avatar.text}</span>
						{:else}
							P{slot}
						{/if}
					</span>
				{/each}
			</span>
		</button>
	{:else}
		<button class="topbar-chip session-start" type="button" disabled={$padAction.busy} on:click={start}>
			<SmartphoneIcon size={17} strokeWidth={2.2} />
			<span>{$padAction.busy ? "Starting…" : "Play on phones"}</span>
		</button>
	{/if}

	{#if open && session}
		<div class="session-popover" role="dialog" aria-label="Phone play">
			<div class="session-join">
				<span class="session-qr">
					<JoinQr value={session.join_url} size={148} label={`QR code to join session ${session.code}`} />
				</span>
				<div class="session-join-copy">
					<span class="session-eyebrow">Scan to join</span>
					<strong class="session-join-code">{session.code}</strong>
					<span class="session-join-host">
						{#if session.lobby_open && joinHost}or visit {joinHost}/play/{session.code}{:else}Point a phone camera at the code{/if}
					</span>
					<button
						class="session-lobby"
						type="button"
						role="switch"
						aria-checked={Boolean(session.lobby_open)}
						disabled={lobbyBusy}
						on:click={toggleLobby}
					>
						<span class="session-switch" class:on={session.lobby_open} aria-hidden="true"></span>
						<span>Let phones join with just the code</span>
					</button>
				</div>
			</div>

			<ul class="session-players" aria-label="Players">
				{#each slots as slot}
					{@const player = playerBySlot.get(slot)}
					{@const avatar = player ? avatarKind(player.avatar) : null}
					<li class="session-player" class:empty={!player}>
						<span class="session-slot" class:has-avatar={avatar} style={player ? `background: ${swatch(player)}` : ""}>
							{#if avatar?.kind === "image"}
								<img class="player-avatar" src={avatar.src} alt="" draggable="false" />
								<span class="session-slot-tag">P{slot}</span>
							{:else if avatar}
								<span class="player-avatar emoji">{avatar.text}</span>
								<span class="session-slot-tag">P{slot}</span>
							{:else}
								P{slot}
							{/if}
						</span>
						{#if player}
							<span class="session-player-name">
								<strong>{player.name}</strong>
								<span>
									{#if !player.connected}Reconnecting…{:else if player.host && player.speaker}Host · sound{channelLetter(player)}{:else if player.host}Host{:else if player.speaker}Sound{channelLetter(player)}{:else}Playing{/if}
									{#if player.connected && linkQuality(player.rtt_ms)}
										<span
											class={`session-link ${linkQuality(player.rtt_ms)}`}
											title={`Round trip ${player.rtt_ms} ms${typeof player.input_hz === "number" ? `, ${player.input_hz} inputs/s` : ""}`}
										>· {player.rtt_ms} ms</span>
									{/if}
								</span>
							</span>
							<span class="session-player-actions">
								{#if !player.speaker && player.connected}
									<button type="button" title={`Play sound on ${player.name}'s phone`} on:click={() => pad.makeSpeaker(player.id)}>
										<Volume2Icon size={17} strokeWidth={2.2} />
									</button>
								{/if}
								{#if !player.speaker && player.connected && hasSpeaker}
									<button
										type="button"
										title={`Pair as stereo: ${player.name}'s phone plays the right channel`}
										on:click={() => pad.makeSpeaker(player.id, "right")}
									>
										L+R
									</button>
								{/if}
								{#if !player.host && player.connected}
									<button type="button" title={`Make ${player.name} the host`} on:click={() => pad.makeHost(player.id)}>
										<CrownIcon size={17} strokeWidth={2.2} />
									</button>
								{/if}
								<button
									type="button"
									class="kick"
									class:confirm={confirmKick === player.id}
									title={`Remove ${player.name}`}
									on:click={() => kick(player)}
								>
									{#if confirmKick === player.id}Remove?{:else}<XIcon size={17} strokeWidth={2.4} />{/if}
								</button>
							</span>
						{:else}
							<span class="session-player-name"><span>Open seat</span></span>
						{/if}
					</li>
				{/each}
			</ul>
			{#if watching.length > 0}
				<p class="session-watching">
					Watching: {watching.map((player) => player.name).join(", ")}
				</p>
			{/if}

			{#if $padAction.error}<p class="session-error" role="alert">{$padAction.error}</p>{/if}

			<button class="session-end" class:confirm={confirmEnd} type="button" disabled={$padAction.busy} on:click={end}>
				{confirmEnd ? "Tap again to end for everyone" : "End phone play"}
			</button>
		</div>
	{:else if !session && $padAction.error}
		<p class="session-toast" role="alert">{$padAction.error}</p>
	{/if}
</div>
