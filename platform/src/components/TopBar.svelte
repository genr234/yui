<script lang="ts">
	import { createEventDispatcher, onMount } from "svelte";
	import DownloadIcon from "lucide-svelte/icons/download";
	import PauseIcon from "lucide-svelte/icons/pause";
	import UserCircleIcon from "lucide-svelte/icons/user-circle";
	import { platformAssetUrl } from "../assets";
	import logoUrl from "../assets/images/logo.png";
	import { assetSync, type AssetSyncStatus } from "../sdk/assets";
	import { pad, type PadState } from "../sdk/pad";
	import SessionChip from "./SessionChip.svelte";
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

	let sync: AssetSyncStatus | null = null;
	let padState: PadState = { session: null, players: [] };
	let arrived: Record<string, boolean> = {};
	let seenPlayers = new Set<string>();
	let playersPrimed = false;
	let accountOpen = false;
	let accountWrap: HTMLDivElement | undefined;

	$: resolvedLogoUrl = platformAssetUrl(logoUrl);
	$: download = sync?.current && sync.current.byte_size > 0 ? sync.current : null;
	$: downloadPercent = download ? Math.min(100, Math.round((download.bytes_done / download.byte_size) * 100)) : 0;
	$: showDownload = Boolean(download) && (sync?.state === "syncing" || sync?.state === "paused");

	onMount(() => {
		void assetSync.status().then((status) => (sync = status));
		void pad.state().then((state) => notePlayers(state));
		const offSync = assetSync.onChange((status) => (sync = status));
		const offPad = pad.onState((state) => notePlayers(state));
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

	function notePlayers(state: PadState) {
		padState = state;
		const connected = state.players.filter((player) => player.connected).map((player) => player.id);
		if (!playersPrimed) {
			playersPrimed = true;
			seenPlayers = new Set(connected);
			return;
		}
		const fresh: Record<string, boolean> = {};
		for (const id of connected) {
			if (!seenPlayers.has(id)) fresh[id] = true;
		}
		seenPlayers = new Set(connected);
		if (Object.keys(fresh).length === 0) return;
		arrived = { ...arrived, ...fresh };
		window.setTimeout(() => {
			const next = { ...arrived };
			for (const id of Object.keys(fresh)) delete next[id];
			arrived = next;
		}, 720);
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

		<SessionChip {padState} {arrived} />

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
