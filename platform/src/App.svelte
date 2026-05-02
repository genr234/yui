<script lang="ts">
	import { onMount } from "svelte";
	import Sidebar from "./components/Sidebar.svelte";
	import TitleBar from "./components/TitleBar.svelte";
	import Router from "./pages/Router.svelte";
	import { findRoute, routes } from "./pages/routes";
	import { bridge } from "./sdk/bridge";
	import type { ActionItem, BridgeState, DetailItem, Section, SectionItem } from "./types";
	import ChromeIcon from "lucide-svelte/icons/chrome";
	import FolderIcon from "lucide-svelte/icons/folder";
	import RefreshCwIcon from "lucide-svelte/icons/refresh-cw";
	import {resolveSubtitle} from "@/pages/subtitle";
	import {store} from "@/sdk/store";

	let open = false;
	let section: Section = "home";
	let status: any = null;
	let diagnostics = "";
	let config: any = null;
	let bridgeState: BridgeState = "connecting";
	let pressTimer: number | undefined;

	const sections: SectionItem[] = routes;

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

	$: activeRoute = findRoute(section);
	$: activeSubtitle = resolveSubtitle(activeRoute, { appCount: 1 });
	$: apps = store.collection<unknown>("apps");
	$: homeActions = [
		{ label: "Refresh Status", icon: RefreshCwIcon, tone: "blue", run: refresh },
		{ label: "Re-import Config", icon: FolderIcon, tone: "green", run: reimportConfig },
		{ label: "Select Chrome", icon: ChromeIcon, tone: "violet", run: selectChrome },
	] satisfies ActionItem[];
	$: toolActions = [
		{ label: "Refresh Diagnostics", icon: RefreshCwIcon, tone: "blue", run: refresh },
		{ label: "Re-import Kiosk Batch", icon: FolderIcon, tone: "green", run: reimportConfig },
		{ label: "Select Chrome", icon: ChromeIcon, tone: "orange", run: selectChrome },
	] satisfies ActionItem[];
	$: settingDetails = [
		{ label: "HTTP", value: metric(config?.platform_http_addr) },
		{ label: "Bridge", value: metric(config?.platform_bridge_addr) },
		{ label: "Debug port", value: metric(config?.platform_remote_debugging_port) },
		{ label: "Chrome", value: metric(config?.chrome_path) },
		{ label: "Config", value: metric(config?.config_path ?? config?.ConfigPath, "Active config loaded") },
		{ label: "Status", value: metric(config?.status_path) },
		{ label: "User data", value: metric(config?.user_data_dir) },
	] satisfies DetailItem[];
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
				<TitleBar title={activeRoute.label} subtitle={activeSubtitle} on:refresh={refresh} />
				<Router {section} {homeActions} {apps} {toolActions} {settingDetails} {diagnostics} />
			</main>
		</div>
	</section>
{/if}
