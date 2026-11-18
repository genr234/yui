<script lang="ts">
	/**
	 * `ctx.ui.camera({ mirror?, fit?, width?, height?, radius? })`: a live
	 * preview of the app's open camera. The stream stays on the host side; the
	 * sandbox only sends these props. Shows a placeholder while the app's
	 * camera is not open.
	 */
	import { onMount } from "svelte";
	import CameraOffIcon from "lucide-svelte/icons/camera-off";
	import { watchCamera, type LiveCamera } from "../../sdk/apps/camera";

	export let appId = "";
	export let mirror = false;
	export let fit: "cover" | "contain" = "cover";
	export let width: string | number = "100%";
	export let height: string | number = "auto";
	export let radius: string | number = 12;

	let camera: LiveCamera | null = null;
	let video: HTMLVideoElement | undefined;

	onMount(() => watchCamera((next) => (camera = next && next.appId === appId ? next : null)));

	// Attach the stream to the video element; detach when it goes away.
	$: if (video) {
		const stream = camera?.stream ?? null;
		if (video.srcObject !== stream) {
			video.srcObject = stream;
			if (stream) void video.play().catch(() => {});
		}
	}

	const css = (value: string | number) => (typeof value === "number" ? `${value}px` : value);
	$: ratio = camera ? `${camera.width} / ${camera.height}` : "4 / 3";
	$: frame = `width: ${css(width)}; height: ${css(height)}; border-radius: ${css(radius)}; aspect-ratio: ${ratio};`;
</script>

<div class="yui-app-camera" style={frame}>
	{#if camera}
		<video
			bind:this={video}
			class:mirror
			style={`object-fit: ${fit === "contain" ? "contain" : "cover"}`}
			muted
			playsinline
			autoplay
		></video>
	{:else}
		<div class="yui-app-camera-off">
			<CameraOffIcon size={28} strokeWidth={2} />
			<span>Camera off</span>
		</div>
	{/if}
</div>
