<script lang="ts">
	import { createEventDispatcher, onDestroy } from "svelte";
	import type { Section, SectionItem, SidebarThemeSettings } from "../types";
	import { platformAssetUrl } from "../assets";
	import logoUrl from "../assets/images/logo.png";
	import UserCircleIcon from "lucide-svelte/icons/user-circle";

	export let sections: SectionItem[] = [];
	export let active: Section = "home";
	export let version = "0.1.0";
	export let accountName = "Anonymous";
	export let accountImage = "";
	export let accountState = "Local only";
	export let accountSyncing = false;
	export let theme: SidebarThemeSettings = { enabled: true, images: [] };

	const dispatch = createEventDispatcher();
	const reduceMotion =
		typeof window !== "undefined" &&
		window.matchMedia("(prefers-reduced-motion: reduce)").matches;

	type SidebarParticle = {
		id: number;
		src: string;
		left: number;
		size: number;
		duration: number;
		drift: number;
		rotate: number;
	};

	let particles: SidebarParticle[] = [];
	let particleTimer: number | undefined;
	let particleSourceKey = "";
	let nextParticleId = 1;

	$: themeActive = theme.enabled && theme.images.length > 0 && !reduceMotion;
	$: resolvedLogoUrl = platformAssetUrl(logoUrl);
	$: syncParticles(themeActive, theme.images.map((image) => image.src));

	onDestroy(() => {
		stopParticles();
	});

	function syncParticles(active: boolean, sources: string[]) {
		const sourceKey = sources.join("\n");
		if (!active) {
			stopParticles();
			particleSourceKey = "";
			particles = [];
			return;
		}
		if (particleSourceKey !== sourceKey) {
			stopParticles();
			particleSourceKey = sourceKey;
		}
		if (particleTimer) return;
		spawnParticle(sources);
		particleTimer = window.setInterval(() => spawnParticle(sources), 950);
	}

	function stopParticles() {
		if (!particleTimer) return;
		window.clearInterval(particleTimer);
		particleTimer = undefined;
	}

	function spawnParticle(sources: string[]) {
		if (sources.length === 0) return;
		const src = sources[Math.floor(Math.random() * sources.length)];
		const id = nextParticleId++;
		const duration = 11000 + Math.random() * 5200;
		particles = [
			...particles.slice(-22),
			{
				id,
				src,
				left: 10 + Math.random() * 80,
				size: 22 + Math.random() * 22,
				duration,
				drift: -18 + Math.random() * 36,
				rotate: -14 + Math.random() * 28,
			},
		];
		window.setTimeout(() => {
			particles = particles.filter((particle) => particle.id !== id);
		}, duration + 250);
	}
</script>

<aside class="sidebar">
	{#if themeActive}
		<div class="sidebar-theme-particles" aria-hidden="true">
			{#each particles as particle (particle.id)}
				<img
					class="sidebar-theme-particle"
					src={particle.src}
					alt=""
					style={`--particle-left: ${particle.left}%; --particle-size: ${particle.size}px; --particle-duration: ${particle.duration}ms; --particle-drift: ${particle.drift}px; --particle-rotate: ${particle.rotate}deg;`}
				/>
			{/each}
		</div>
	{/if}

	<div class="sidebar-top">
		<div class="brand">
			<span class="brand-logo-frame">
				<img class="brand-logo" src={resolvedLogoUrl} alt="Yui" />
			</span>
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
