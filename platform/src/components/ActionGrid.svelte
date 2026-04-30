<script lang="ts">
	import type { ActionItem } from "../types";

	export let actions: ActionItem[] = [];
</script>

<div class="quick-grid">
	{#each actions as action}
		<button class="action-card {action.tone}" on:click={action.run}>
			<span class="big-icon"><svelte:component this={action.icon} /></span>
			<strong>{action.label}</strong>
		</button>
	{/each}
</div>
