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
	 * when it comes up, and the play QR crossfades when its token rotates.
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
						{#if wifi.password}<small>Password <span class="join-step-pass">{wifi.password}</span></small>{/if}
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

<style>
	.join-steps {
		display: flex;
		align-items: flex-start;
		justify-content: center;
		gap: 10px;
		color: var(--yui-ink);
	}

	.join-step {
		display: grid;
		justify-items: center;
		gap: 10px;
		width: var(--join-tile);
		margin: 0;
	}

	.join-step-tile {
		display: grid;
		width: var(--join-tile);
		aspect-ratio: 1;
		overflow: hidden;
		border: 1px solid var(--yui-line);
		border-radius: 14px;
		background: #ffffff;
		box-shadow: 0 var(--yui-lip, 3px) 0 var(--yui-line);
	}

	/* Old and new codes share one cell while they crossfade. */
	.join-step-layer {
		grid-area: 1 / 1;
		display: grid;
		place-items: center;
		min-width: 0;
		min-height: 0;
	}

	.join-step-layer :global(svg) {
		display: block;
		width: 100%;
		height: 100%;
	}

	.join-step-wait {
		align-content: center;
		gap: 10px;
		padding: 12px;
		color: var(--yui-muted);
		background: var(--yui-surface);
		font-size: 13px;
		font-weight: 650;
		text-align: center;
		animation: join-step-pulse 1.6s ease-in-out infinite;
	}

	figcaption {
		display: flex;
		align-items: flex-start;
		gap: 8px;
		max-width: 100%;
		min-width: 0;
	}

	.join-step-num {
		display: grid;
		flex: 0 0 auto;
		place-items: center;
		width: 22px;
		height: 22px;
		margin-top: 1px;
		border-radius: 50%;
		color: #ffffff;
		background: var(--yui-blue);
		font-size: 12px;
		font-weight: 800;
	}

	.join-step-copy {
		display: grid;
		gap: 1px;
		min-width: 0;
		line-height: 1.25;
	}

	.join-step-copy strong {
		font-size: 14px;
		font-weight: 750;
	}

	.join-step-copy small {
		overflow: hidden;
		color: var(--yui-muted);
		font-size: 12px;
		font-weight: 550;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.join-step-ssid {
		color: var(--yui-ink-2, var(--yui-ink)) !important;
		font-weight: 700 !important;
	}

	.join-step-pass {
		font-family: ui-monospace, "SF Mono", Menlo, Consolas, monospace;
		letter-spacing: 0.04em;
	}

	.join-step-arrow {
		display: grid;
		place-items: center;
		height: var(--join-tile);
		color: var(--yui-muted);
	}

	/* The attract screen: white tiles on the navy chrome. */
	.dark {
		gap: clamp(14px, 1.6vw, 28px);
		color: var(--yui-chrome-fg);
	}

	.dark .join-step-tile {
		border: 0;
		border-radius: var(--yui-radius-hero, 24px);
		box-shadow:
			0 var(--yui-lip, 4px) 0 rgba(0, 0, 0, 0.28),
			var(--yui-shadow-hero, 0 20px 50px rgba(0, 0, 0, 0.35));
	}

	.dark .join-step-wait {
		background: rgba(255, 255, 255, 0.92);
		font-size: clamp(14px, 1.1vw, 18px);
	}

	.dark .join-step-copy strong {
		font-size: clamp(16px, 1.3vw, 22px);
	}

	.dark .join-step-copy small {
		color: var(--yui-chrome-muted);
		font-size: clamp(13px, 1vw, 17px);
	}

	.dark .join-step-ssid {
		color: var(--yui-chrome-fg) !important;
	}

	.dark .join-step-num {
		width: 28px;
		height: 28px;
		font-size: 14px;
	}

	.dark .join-step-arrow {
		color: var(--yui-chrome-muted);
	}

	@keyframes join-step-pulse {
		0%,
		100% {
			opacity: 1;
		}
		50% {
			opacity: 0.55;
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.join-step-wait {
			animation: none;
		}
	}
</style>
