<script lang="ts">
	import { fade } from "svelte/transition";
	import ArrowRightIcon from "lucide-svelte/icons/arrow-right";
	import WifiIcon from "lucide-svelte/icons/wifi";
	import SmartphoneIcon from "lucide-svelte/icons/smartphone";
	import JoinQr from "./JoinQr.svelte";
	import type { PadState } from "../sdk/pad";

	/**
	 * The QR side of joining a play session, shared by the top bar popover,
	 * the attract screen and app join cards. With the kiosk hotspot on (or
	 * starting) it is two steps: join the Wi-Fi, then scan to play. The
	 * Wi-Fi tile holds its place while the hotspot starts, so nothing jumps
	 * when it comes up, and the play QR crossfades if its URL changes.
	 */
	export let session: NonNullable<PadState["session"]>;
	/** Side of each QR tile: px, or any CSS length (a clamp() for the attract screen). */
	export let size: number | string = 148;
	/** "dark" for the navy chrome (attract screen); "light" on white cards. */
	export let tone: "light" | "dark" = "light";

	$: tile = typeof size === "number" ? `${size}px` : size;
	$: wifiStep = Boolean(session.wifi || session.wifi_pending);
	$: wifi = session.wifi;
</script>

<div class="join-steps" class:dark={tone === "dark"} style={`--join-tile: ${tile}`}>
	{#if wifiStep}
		<figure class="join-step">
			<div class="join-step-tile">
				{#if wifi}
					{#key wifi.qr}
						<div class="join-step-layer" in:fade={{ duration: 220 }} out:fade={{ duration: 220 }}>
							<JoinQr value={wifi.qr} size={240} label={`QR code to join the Wi-Fi network ${wifi.ssid}`} />
						</div>
					{/key}
				{:else}
					<div class="join-step-layer join-step-wait" out:fade={{ duration: 160 }}>
						<WifiIcon size={28} strokeWidth={2.2} />
						<span>Starting Wi-Fi…</span>
					</div>
				{/if}
			</div>
			<figcaption>
				<span class="join-step-num">1</span>
				<span class="join-step-copy">
					<strong>Join Wi-Fi</strong>
					{#if wifi}
						<small class="join-step-ssid">{wifi.ssid}</small>
						{#if wifi.password}<small class="join-step-passline">Password <span class="join-step-pass">{wifi.password}</span></small>{/if}
					{:else}
						<small>Faster controls</small>
					{/if}
				</span>
			</figcaption>
		</figure>
		<span class="join-step-arrow" aria-hidden="true"><ArrowRightIcon size={22} strokeWidth={2.4} /></span>
	{/if}

	<figure class="join-step">
		<div class="join-step-tile">
			{#if session.join_url}
				{#key session.join_url}
					<div class="join-step-layer" in:fade={{ duration: 220 }} out:fade={{ duration: 220 }}>
						<JoinQr value={session.join_url} size={240} label={`QR code to join session ${session.code}`} />
					</div>
				{/key}
			{:else}
				<div class="join-step-layer join-step-wait" out:fade={{ duration: 160 }}>
					<SmartphoneIcon size={28} strokeWidth={2.2} />
					<span>Getting a code ready…</span>
				</div>
			{/if}
		</div>
		<figcaption>
			{#if wifiStep}<span class="join-step-num">2</span>{/if}
			<span class="join-step-copy">
				<strong>Scan to play</strong>
				<small>Phone camera, no app</small>
			</span>
		</figcaption>
	</figure>
</div>

<!-- Styles live in styles/join.css: the overlay renders in a shadow root, which a
     component <style> block never reaches (main.ts inlines the styles/ files). -->
