<script lang="ts">
	import type { DetailItem } from "../types";

	export let title = "";
	export let details: DetailItem[] = [];
</script>

<section class="card detail-list">
	<div class="card-title">{title}</div>
	<dl>
		{#each details as detail}
			<div>
				<dt>{detail.label}</dt>
				<dd>{detail.value}</dd>
			</div>
		{/each}
	</dl>
</section>
