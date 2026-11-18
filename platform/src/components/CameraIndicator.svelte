<script lang="ts">
	import { onMount } from "svelte";
	import CameraIcon from "lucide-svelte/icons/camera";
	import { watchCamera, type LiveCamera } from "../sdk/apps/camera";

	// Drawn by the platform, outside the app layer, above everything and never
	// hit-testable: an app can neither cover nor remove it while the camera runs.
	let camera: LiveCamera | null = null;

	onMount(() => watchCamera((next) => (camera = next)));
</script>

{#if camera}
	<div class="camera-indicator" role="status" aria-live="polite">
		<span class="camera-indicator-dot" aria-hidden="true"></span>
		<CameraIcon size={16} strokeWidth={2.4} />
		<span class="camera-indicator-text">Camera on</span>
		<span class="camera-indicator-app">{camera.appName}</span>
	</div>
{/if}
