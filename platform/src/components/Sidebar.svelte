<script lang="ts">
	import { createEventDispatcher } from "svelte";
	import type { Section, SectionItem } from "../types";
	import UserCircleIcon from "lucide-svelte/icons/user-circle";

	export let sections: SectionItem[] = [];
	export let active: Section = "home";
	export let version = "0.1.0";
	export let accountName = "Anonymous";
	export let accountImage = "";
	export let accountState = "Local only";
	export let accountSyncing = false;

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

	<div class="sidebar-bottom">
		<button class="account-summary" on:click={() => dispatch("account")}>
			<span class="account-avatar" aria-hidden="true">
				{#if accountImage}
					<img src={accountImage} alt="" />
				{:else}
					<UserCircleIcon size={22} strokeWidth={2.1} />
				{/if}
			</span>
			<span>
				<span>{accountName}</span>
				<small>{accountSyncing ? "Syncing" : accountState}</small>
			</span>
		</button>
	</div>
</aside>
