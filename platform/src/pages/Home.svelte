<script lang="ts">
	import ActionGrid from "../components/ActionGrid.svelte";
	import type { ActionItem } from "../types";

	export let actions: ActionItem[] = [];
</script>

<section>
	<div class="section-heading">
		<h2>Quick Actions</h2>
	</div>
	<ActionGrid {actions} />
</section>
