<script lang="ts">
	import ActionGrid from "../components/ActionGrid.svelte";
	import type { ActionItem } from "../types";

	export let actions: ActionItem[] = [];
	export let diagnostics = "";
</script>

<ActionGrid {actions} />

<section class="card">
	<div class="card-title">Diagnostics</div>
	<pre>{diagnostics || "No diagnostics yet."}</pre>
</section>
