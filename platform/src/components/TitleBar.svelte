<script lang="ts">
	import { createEventDispatcher } from "svelte";

	export let title = "";
	export let subtitle = "";

	const dispatch = createEventDispatcher();
</script>

<div class="title-row">
	<div>
		<h1>{title}</h1>
		<p class="subtitle">{subtitle}</p>
	</div>
</div>
