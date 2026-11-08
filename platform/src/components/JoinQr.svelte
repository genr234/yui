<script lang="ts">
	import qrcode from "qrcode-generator";

	/** Text to encode (the join URL). */
	export let value = "";
	export let size = 44;
	export let label = "QR code to join";

	let path = "";
	let count = 0;

	$: render(value);

	function render(text: string) {
		if (!text) {
			path = "";
			count = 0;
			return;
		}
		const qr = qrcode(0, "M");
		qr.addData(text);
		qr.make();
		count = qr.getModuleCount();
		let d = "";
		for (let row = 0; row < count; row += 1) {
			for (let col = 0; col < count; col += 1) {
				if (qr.isDark(row, col)) d += `M${col} ${row}h1v1h-1z`;
			}
		}
		path = d;
	}
</script>

{#if path}
	<svg
		class="join-qr"
		role="img"
		aria-label={label}
		width={size}
		height={size}
		viewBox={`-2 -2 ${count + 4} ${count + 4}`}
		shape-rendering="crispEdges"
	>
		<rect x="-2" y="-2" width={count + 4} height={count + 4} fill="#ffffff" rx="2" />
		<path d={path} fill="#111925" />
	</svg>
{/if}
