<script lang="ts">
	import { onDestroy } from "svelte";
	import type { SidebarThemeSettings } from "../types";

	/** Theme images that float up and fade (Settings → Theme images). */
	export let theme: SidebarThemeSettings = { enabled: true, images: [] };

	type Particle = {
		id: number;
		src: string;
		left: number;
		size: number;
		duration: number;
		drift: number;
		rotate: number;
	};

	const reduceMotion =
		typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

	let particles: Particle[] = [];
	let timer: number | undefined;
	let sourceKey = "";
	let nextId = 1;

	$: active = theme.enabled && theme.images.length > 0 && !reduceMotion;
	$: sync(active, theme.images.map((image) => image.src));

	onDestroy(stop);

	function sync(on: boolean, sources: string[]) {
		const key = sources.join("\n");
		if (!on) {
			stop();
			sourceKey = "";
			particles = [];
			return;
		}
		if (sourceKey !== key) {
			stop();
			sourceKey = key;
		}
		if (timer) return;
		spawn(sources);
		timer = window.setInterval(() => spawn(sources), 950);
	}

	function stop() {
		if (!timer) return;
		window.clearInterval(timer);
		timer = undefined;
	}

	function spawn(sources: string[]) {
		if (sources.length === 0) return;
		const src = sources[Math.floor(Math.random() * sources.length)];
		const id = nextId++;
		const duration = 9000 + Math.random() * 4200;
		particles = [
			...particles.slice(-22),
			{
				id,
				src,
				left: 4 + Math.random() * 92,
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

{#if active}
	<div class="theme-particles" aria-hidden="true">
		{#each particles as particle (particle.id)}
			<img
				class="theme-particle"
				src={particle.src}
				alt=""
				style={`--particle-left: ${particle.left}%; --particle-size: ${particle.size}px; --particle-duration: ${particle.duration}ms; --particle-drift: ${particle.drift}px; --particle-rotate: ${particle.rotate}deg;`}
			/>
		{/each}
	</div>
{/if}
