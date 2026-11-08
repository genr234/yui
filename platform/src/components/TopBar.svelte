<script lang="ts">
	import { createEventDispatcher, onMount } from "svelte";
	import DownloadIcon from "lucide-svelte/icons/download";
	import PauseIcon from "lucide-svelte/icons/pause";
	import UserCircleIcon from "lucide-svelte/icons/user-circle";
	import { platformAssetUrl } from "../assets";
	import logoUrl from "../assets/images/logo.png";
	import { assetSync, type AssetSyncStatus } from "../sdk/assets";
	import { pad, playerSwatch, type PadState } from "../sdk/pad";
	import type { Section, SectionItem } from "../types";

	export let sections: SectionItem[] = [];
	export let active: Section = "home";
	export let version = "0.1.0";
	export let accountName = "Anonymous";
	export let accountImage = "";
	export let accountState = "Local only";
	export let accountServer = "Local only";
	export let accountSyncLabel = "Not synced yet";
	export let accountSyncError = false;
	export let accountSyncing = false;

	const dispatch = createEventDispatcher<{ select: Section; account: void }>();
	const slots = [1, 2, 3, 4];

	let sync: AssetSyncStatus | null = null;
	let padState: PadState = { session: null, players: [] };
	let accountOpen = false;
	let accountWrap: HTMLDivElement | undefined;

	$: resolvedLogoUrl = platformAssetUrl(logoUrl);
	$: download = sync?.current && sync.current.byte_size > 0 ? sync.current : null;
	$: downloadPercent = download ? Math.min(100, Math.round((download.bytes_done / download.byte_size) * 100)) : 0;
	$: showDownload = Boolean(download) && (sync?.state === "syncing" || sync?.state === "paused");
	$: playerBySlot = new Map(padState.players.filter((player) => player.slot).map((player) => [player.slot, player]));

	onMount(() => {
		void assetSync.status().then((status) => (sync = status));
		void pad.state().then((state) => (padState = state));
		const offSync = assetSync.onChange((status) => (sync = status));
		const offPad = pad.onState((state) => (padState = state));
		const onPointerDown = (event: PointerEvent) => {
			if (!accountOpen || !accountWrap) return;
			if (event.composedPath().includes(accountWrap)) return;
			accountOpen = false;
		};
		const onKeyDown = (event: KeyboardEvent) => {
			if (!accountOpen || event.key !== "Escape") return;
			event.stopPropagation();
			accountOpen = false;
		};
		window.addEventListener("pointerdown", onPointerDown);
		window.addEventListener("keydown", onKeyDown);
		return () => {
			offSync();
			offPad();
			window.removeEventListener("pointerdown", onPointerDown);
			window.removeEventListener("keydown", onKeyDown);
		};
	});

	function openAccountSettings() {
		accountOpen = false;
		dispatch("account");
	}

	function openAbout() {
		accountOpen = false;
		dispatch("select", "about");
	}

	function playerTitle(slot: number) {
		const player = playerBySlot.get(slot);
		if (!player) return `Player ${slot}: open`;
		const roles = [player.host && "host", player.speaker && "speaker"].filter(Boolean).join(", ");
		return `Player ${slot}: ${player.name}${roles ? ` (${roles})` : ""}`;
	}
</script>

<header class="topbar">
	<div class="topbar-start">
		<div class="brand">
			<span class="brand-logo-frame">
				<img class="brand-logo" src={resolvedLogoUrl} alt="Yui" />
			</span>
			<span class="brand-version">v{version}</span>
		</div>

		<nav class="topbar-tabs" aria-label="Sections">
			{#each sections as item}
				<button
					class="topbar-tab"
					class:active={active === item.id}
					aria-current={active === item.id ? "page" : undefined}
					on:click={() => dispatch("select", item.id)}
				>
					<svelte:component this={item.icon} size={19} strokeWidth={1.9} />
					<span>{item.label}</span>
				</button>
			{/each}
		</nav>
	</div>

	<div class="topbar-end">
		{#if showDownload && download}
			<div
				class="topbar-chip"
				title={sync?.state === "paused" ? "Downloads paused while people are playing" : "Downloading to this kiosk"}
			>
				{#if sync?.state === "paused"}
					<PauseIcon size={16} strokeWidth={2.2} />
				{:else}
					<DownloadIcon size={16} strokeWidth={2.2} />
				{/if}
				<span>{sync?.state === "paused" ? "Paused" : `${downloadPercent}%`}</span>
				<span class="topbar-meter" aria-hidden="true"><span style={`width: ${downloadPercent}%`}></span></span>
			</div>
		{/if}

		{#if padState.session}
			<div class="topbar-chip players" aria-label={`Players: ${padState.players.length} of 4`}>
				{#each slots as slot}
					{@const player = playerBySlot.get(slot)}
					<span
						class="player-square"
						class:open={!player}
						class:away={player && !player.connected}
						title={playerTitle(slot)}
						style={player ? `background: ${playerSwatch[player.color] ?? "#2f76dc"}` : ""}
					>
						P{slot}
					</span>
				{/each}
			</div>
		{/if}

		<div class="topbar-account-wrap" bind:this={accountWrap}>
			<button
				class="topbar-account"
				title={`${accountName} · ${accountSyncing ? "Syncing" : accountState}`}
				aria-expanded={accountOpen}
				aria-haspopup="dialog"
				on:click={() => (accountOpen = !accountOpen)}
			>
				<span class="topbar-avatar" aria-hidden="true">
					{#if accountImage}
						<img src={accountImage} alt="" />
					{:else}
						<UserCircleIcon size={22} strokeWidth={2} />
					{/if}
					{#if accountSyncing}<span class="topbar-avatar-dot"></span>{/if}
				</span>
				<span class="topbar-account-name">{accountName}</span>
			</button>
			{#if accountOpen}
				<div class="account-popover" role="dialog" aria-label="Account">
					<div class="account-popover-identity">
						<span class="account-popover-avatar" aria-hidden="true">
							{#if accountImage}
								<img src={accountImage} alt="" />
							{:else}
								<UserCircleIcon size={26} strokeWidth={2} />
							{/if}
						</span>
						<span class="account-popover-copy">
							<strong>{accountName}</strong>
							<span>{accountServer}</span>
							<span class:error={accountSyncError}>{accountSyncLabel}</span>
						</span>
					</div>
					<button class="account-popover-action" type="button" on:click={openAccountSettings}>Account settings</button>
					<button class="account-popover-action" type="button" on:click={openAbout}>About</button>
				</div>
			{/if}
		</div>
	</div>
</header>
