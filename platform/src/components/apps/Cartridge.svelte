<script lang="ts">
	import { fallbackAppIcon, isImageAppIcon } from "../../apps";
	import type { YuiDevApp } from "../../sdk/apps";

	export let app: YuiDevApp;
	export let size: "tile" | "hero" = "tile";
</script>

<span class={`cartridge ${size}`} aria-hidden="true">
	<span class="cartridge-brand">YUI</span>
	<span class="cartridge-ridges left"></span>
	<span class="cartridge-ridges right"></span>
	<span class="cartridge-label">
		{#if isImageAppIcon(app.app.icon)}
			<img src={app.app.icon} alt="" />
		{:else}
			<span>{fallbackAppIcon(app)}</span>
		{/if}
	</span>
	<span class="cartridge-notch"></span>
</span>
