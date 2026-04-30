<script lang="ts">
	import { createEventDispatcher } from "svelte";
	import type { BridgeState, Section, SectionItem } from "../types";

	export let sections: SectionItem[] = [];
	export let active: Section = "home";
	export let version = "0.1.0";

	const dispatch = createEventDispatcher();
</script>

<aside class="sidebar">
	<div class="sidebar-top">
		<div class="brand">
			<div class="brand-name">Yui</div>
			<div class="brand-version">v{version}</div>
		</div>

		<nav class="nav" aria-label="Sections">
			{#each sections as item}
				<button class:active={active === item.id} on:click={() => dispatch("select", item.id)}>
					<svelte:component this={item.icon} />
					<span>{item.label}</span>
				</button>
			{/each}
		</nav>
	</div>
</aside>
