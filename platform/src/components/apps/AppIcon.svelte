<script lang="ts">
	import { fallbackAppIcon, isImageAppIcon } from "../../apps";
	import type { YuiDevApp } from "../../sdk/apps";

	export let app: YuiDevApp;
	export let size = 64;
</script>

{#if isImageAppIcon(app.app.icon)}
	<img class="app-icon-image" src={app.app.icon} alt="" width={size} height={size} />
{:else}
	<span class="app-icon-text" style={`font-size: ${Math.round(size * 0.5)}px`}>{fallbackAppIcon(app)}</span>
{/if}
