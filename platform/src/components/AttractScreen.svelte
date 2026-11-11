<script lang="ts">
	import { onDestroy } from "svelte";
	import JoinQr from "./JoinQr.svelte";
	import WifiQr from "./WifiQr.svelte";
	import AppIcon from "./apps/AppIcon.svelte";
	import { nextSlide, type ShowcaseEntry } from "../attract";
	import type { YuiDevApp } from "../sdk/apps";

	/** Event mode's full-screen loop. App.svelte decides when it is shown. */
	export let shown = false;
	export let session: { code: string; join_url: string; wifi?: { ssid: string; qr: string } } | null = null;
	export let entries: ShowcaseEntry<YuiDevApp>[] = [];

	/** How long each cover stays up. */
	const SLIDE_MS = 5200;
	/** Fade-out time; the layer keeps eating pointer events until it ends. */
	const LEAVE_MS = 280;

	let visible = false;
	let leaving = false;
	let index = 0;
	let slideTimer: number | undefined;
	let leaveTimer: number | undefined;

	$: if (shown) enter();
	else leave();
	$: if (index >= entries.length) index = 0;
	$: current = entries[index] ?? null;
	$: host = session ? hostOf(session.join_url) : "";

	function enter() {
		window.clearTimeout(leaveTimer);
		leaving = false;
		visible = true;
		if (slideTimer === undefined) {
			slideTimer = window.setInterval(() => {
				index = nextSlide(index, entries.length);
			}, SLIDE_MS);
		}
	}

	function leave() {
		if (slideTimer !== undefined) {
			window.clearInterval(slideTimer);
			slideTimer = undefined;
		}
		if (!visible) return;
		leaving = true;
		window.clearTimeout(leaveTimer);
		leaveTimer = window.setTimeout(() => {
			visible = false;
			leaving = false;
		}, LEAVE_MS);
	}

	function hostOf(url: string) {
		try {
			return new URL(url).host;
		} catch {
			return "";
		}
	}

	onDestroy(() => {
		window.clearInterval(slideTimer);
		window.clearTimeout(leaveTimer);
	});
</script>

{#if visible}
	<div class="attract" class:leaving role="presentation" aria-label="Play with your phone">
		<div class="attract-glow attract-glow-a"></div>
		<div class="attract-glow attract-glow-b"></div>

		<div class="attract-copy">
			<span class="attract-brand">Yui</span>
			<h1>Play with your phone</h1>
			<p class="attract-sub">No app, no account. Point your camera at the code.</p>
			<div class="attract-join">
				{#if session?.wifi}
					<div class="attract-qr">
						<WifiQr wifi={session.wifi} size={220} />
					</div>
				{/if}
				<div class="attract-qr">
					{#if session?.join_url}
						<JoinQr value={session.join_url} size={300} label="QR code to join this kiosk" />
					{:else}
						<span class="attract-qr-wait">Getting a code ready</span>
					{/if}
				</div>
				{#if session}
					<div class="attract-code">
						<small>Or go to {host || "the join page"} and enter</small>
						<strong>{session.code}</strong>
					</div>
				{/if}
			</div>
		</div>

		<div class="attract-stage" aria-hidden="true">
			{#key current?.key}
				{#if current}
					<figure class="attract-card">
						<div class="attract-art">
							{#if current.art}
								<img src={current.art} alt="" />
							{:else}
								<span class="attract-icon"><AppIcon app={current.app} size={200} /></span>
							{/if}
						</div>
						<figcaption>{current.name}</figcaption>
					</figure>
				{/if}
			{/key}
		</div>

		<p class="attract-hint">Touch the screen to start browsing</p>
	</div>
{/if}
