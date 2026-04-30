<script lang="ts">
	export let title = "";
	export let body = "";
</script>

<section class="card empty-state">
	<h2>{title}</h2>
	<p>{body}</p>
	<slot />
</section>
