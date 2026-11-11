<script lang="ts">
	import JoinQr from "./JoinQr.svelte";

	/** The kiosk hotspot from the session (`session.wifi`). */
	export let wifi: { ssid: string; qr: string };
	export let size = 132;
</script>

<!-- Step 1 next to the pad QR: joining the kiosk's own Wi-Fi gives phones a
     direct, low-lag link. It is optional; phones on any network still play. -->
<figure class="wifi-qr">
	<JoinQr value={wifi.qr} {size} label={`QR code to join the Wi-Fi network ${wifi.ssid}`} />
	<figcaption>
		<span>1 · Join Wi-Fi</span>
		<strong>{wifi.ssid}</strong>
	</figcaption>
</figure>

<style>
	.wifi-qr {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.4rem;
		margin: 0;
	}
	figcaption {
		display: flex;
		flex-direction: column;
		align-items: center;
		font-size: 0.85em;
		line-height: 1.2;
		text-align: center;
	}
	figcaption span {
		opacity: 0.7;
	}
</style>
