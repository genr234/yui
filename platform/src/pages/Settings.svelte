<script lang="ts">
	import DetailCard from "../components/DetailCard.svelte";
	import type { DetailItem } from "../types";

	export let details: DetailItem[] = [];
</script>

<DetailCard title="Platform Settings" {details} />
