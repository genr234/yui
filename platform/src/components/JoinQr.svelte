<script lang="ts">
	import qrcode from "qrcode-generator";

	/** Text to encode (the join URL, or a WIFI: code). */
	export let value = "";
	export let size = 44;
	export let label = "QR code to join";

	/**
	 * Light modules around the code. The QR spec asks for four; with less,
	 * phone cameras miss the code on a busy or dark screen behind it. Only a
	 * thumbnail that is not meant to be scanned should go lower.
	 */
	export let quiet = 4;

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
		// One rectangle per run of dark modules: fewer edges, so no hairline
		// seams between modules at fractional scales.
		let d = "";
		for (let row = 0; row < count; row += 1) {
			let col = 0;
			while (col < count) {
				if (!qr.isDark(row, col)) {
					col += 1;
					continue;
				}
				const start = col;
				while (col < count && qr.isDark(row, col)) col += 1;
				d += `M${start} ${row}h${col - start}v1h-${col - start}z`;
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
		viewBox={`${-quiet} ${-quiet} ${count + quiet * 2} ${count + quiet * 2}`}
		shape-rendering="crispEdges"
	>
		<rect x={-quiet} y={-quiet} width={count + quiet * 2} height={count + quiet * 2} fill="#ffffff" />
		<path d={path} fill="#111925" />
	</svg>
{/if}
