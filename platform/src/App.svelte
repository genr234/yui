<script lang="ts">
	import { onMount } from "svelte";
	import ActionGrid from "./components/ActionGrid.svelte";
	import DetailCard from "./components/DetailCard.svelte";
	import EmptyState from "./components/EmptyState.svelte";
	import Sidebar from "./components/Sidebar.svelte";
	import TitleBar from "./components/TitleBar.svelte";
	import { bridge } from "./sdk/bridge";
	import type { ActionItem, BridgeState, DetailItem, MetricItem, Section, SectionItem } from "./types";
	import HouseIcon from "lucide-svelte/icons/house";
	import InfoIcon from "lucide-svelte/icons/info";
	import LayoutGridIcon from "lucide-svelte/icons/layout-grid";
	import SettingsIcon from "lucide-svelte/icons/settings";
	import WrenchIcon from "lucide-svelte/icons/wrench";

	let open = false;
	let section: Section = "home";
	let status: any = null;
	let diagnostics = "";
	let config: any = null;
	let bridgeState: BridgeState = "connecting";
	let pressTimer: number | undefined;

	const sections: SectionItem[] = [
		{ id: "home", label: "Home", icon: HouseIcon },
		{ id: "apps", label: "Apps", icon: LayoutGridIcon },
		{ id: "tools", label: "Tools", icon: WrenchIcon },
		{ id: "settings", label: "Settings", icon: SettingsIcon },
		{ id: "about", label: "About", icon: InfoIcon },
	];

	onMount(() => {
		void refresh();
	});

	async function refresh() {
		try {
			const [statusResult, diagnosticsResult, configResult] = await Promise.all([
				bridge.send<any>("status.get"),
				bridge.send<{ text: string }>("diagnostics.get"),
				bridge.send<any>("config.get"),
			]);
			status = statusResult;
			diagnostics = diagnosticsResult?.text ?? "";
			config = configResult;
			bridgeState = "online";
		} catch (error) {
			bridgeState = "offline";
			diagnostics = error instanceof Error ? error.message : String(error);
		}
	}

	function startPress() {
		clearPress();
		pressTimer = window.setTimeout(() => {
			open = true;
			void refresh();
		}, 850);
	}

	function clearPress() {
		if (pressTimer) {
			window.clearTimeout(pressTimer);
			pressTimer = undefined;
		}
	}

	async function reimportConfig() {
		await bridge.send("platform.reimport");
		await refresh();
	}

	async function selectChrome() {
		await bridge.send("platform.selectChrome");
		await refresh();
	}

	function metric(value: unknown, fallback = "Unknown") {
		return value === undefined || value === null || value === "" ? fallback : String(value);
	}

	$: activeLabel = sections.find((item) => item.id === section)?.label ?? "Home";
	$: activeSubtitle = sections.find((item) => item.id === section)?.label ?? "Home";
	$: homeMetrics = [
	] satisfies MetricItem[];
	$: homeActions = [
	] satisfies ActionItem[];
	$: toolActions = [
	] satisfies ActionItem[];
	$: runtimeDetails = [
	] satisfies DetailItem[];
	$: settingDetails = [
		{ label: "HTTP", value: metric(config?.platform_http_addr) },
		{ label: "Bridge", value: metric(config?.platform_bridge_addr) },
		{ label: "Debug port", value: metric(config?.platform_remote_debugging_port) },
		{ label: "Chrome", value: metric(config?.chrome_path) },
		{ label: "Config", value: metric(config?.config_path ?? config?.ConfigPath, "Active config loaded") },
		{ label: "Status", value: metric(config?.status_path) },
		{ label: "User data", value: metric(config?.user_data_dir) },
		{ label: "Refresh Diagnostics", icon: "refresh", tone: "blue", run: refresh },
		{ label: "Re-import Kiosk Batch", icon: "folder", tone: "green", run: reimportConfig },
		{ label: "Select Chrome", icon: "device", tone: "orange", run: selectChrome },
	];
</script>

<div
	class="hotspot"
	aria-label="Open Yui"
	role="button"
	tabindex="0"
	on:pointerdown={startPress}
	on:pointerup={clearPress}
	on:pointercancel={clearPress}
	on:pointerleave={clearPress}
	on:keydown={(event) => event.key === "Enter" && (open = true)}
></div>

{#if open || localStorage.getItem("yui_always_open") === "true"}
	<button class="veil" aria-label="Close Yui menu" on:click={() => (open = false)}></button>

	<section class="shell" aria-label="Yui Platform">
		<Sidebar
			{sections}
			active={section}
			version={status?.version ?? "0.1.0"}
			on:select={(event) => (section = event.detail)}
		/>

		<div class="workspace">

			<main class="main">
				<TitleBar title={activeLabel} subtitle={activeSubtitle} on:refresh={refresh} />

				{#if section === "home"}
					<section>
						<div class="section-heading">
							<h2>Quick Actions</h2>
						</div>
						<ActionGrid actions={homeActions} />
					</section>
				{:else if section === "apps"}
					<EmptyState
							title="Games"
							body=""
					/>
					<EmptyState
							title="Utilities"
							body=""
					/>
					<EmptyState
							title="Explore"
							body=""
					/>
				{:else if section === "tools"}
					<ActionGrid actions={toolActions} />
					<section class="card">
						<div class="card-title">Diagnostics</div>
						<pre>{diagnostics || "No diagnostics yet."}</pre>
					</section>
				{:else if section === "settings"}
					<DetailCard title="Platform Settings" />
				{:else}
					<EmptyState title="About Yui" body="Yui is a fully featured DigiKiosk jailbreak that adds cool things i always dreamed to have on public kiosks">
					</EmptyState>
				{/if}
			</main>
		</div>

	</section>
{/if}
