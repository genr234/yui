<script lang="ts">
	import { createEventDispatcher, onDestroy, onMount } from "svelte";
	import ActivityIcon from "lucide-svelte/icons/activity";
	import BlocksIcon from "lucide-svelte/icons/blocks";
	import LayoutGridIcon from "lucide-svelte/icons/layout-grid";
	import PlusIcon from "lucide-svelte/icons/plus";
	import { fallbackAppIcon, isImageAppIcon, loadApps } from "../apps";
	import { plugins, type YuiPlugin } from "../sdk/plugins";
	import type { ActionItem, Section } from "../types";
	import type { YuiDevApp } from "../sdk/apps";

	export let actions: ActionItem[] = [];
	export let editing = false;

	type HomeWidgetKind =
		| "apps"
		| "app"
		| "plugin-status"
		| "plugin"
		| "action";

	type HomeWidget = {
		id: string;
		kind: HomeWidgetKind;
		title: string;
		subtitle: string;
		target?: string;
	};

	const widgetStorageKey = "yui_home_widgets_v1";
	const dispatch = createEventDispatcher<{
		launchApp: { appId: string };
		navigate: { section: Section };
		pluginSettings: { pluginId: string };
	}>();

	let apps: YuiDevApp[] = [];
	let installedPlugins: YuiPlugin[] = [];
	let loading = true;
	let error = "";
	let placedWidgetIds: string[] = [];
	let draggedWidgetId = "";
	let dragOverWidgetId = "";
	let refreshTimer: number | undefined;

	onMount(() => {
		placedWidgetIds = loadPlacedWidgetIds();
		void refreshWidgets();
		const refresh = () => {
			window.clearTimeout(refreshTimer);
			refreshTimer = window.setTimeout(() => {
				void refreshWidgets();
			}, 40);
		};
		window.addEventListener("yui:plugins-changed", refresh);
		window.addEventListener("yui:apps-changed", refresh);
		return () => {
			window.clearTimeout(refreshTimer);
			window.removeEventListener("yui:plugins-changed", refresh);
			window.removeEventListener("yui:apps-changed", refresh);
		};
	});

	onDestroy(() => {
		window.clearTimeout(refreshTimer);
	});

	$: runningPlugins = installedPlugins.filter((plugin) => plugin.enabled).length;
	$: erroredPlugins = installedPlugins.filter((plugin) => plugin.lastError).length;
	$: systemWidgets = [
		{
			id: "apps",
			kind: "apps",
			title: "Apps dock",
			subtitle: apps.length === 1 ? "1 installed app" : `${apps.length} installed apps`,
		},
		{
			id: "plugin-status",
			kind: "plugin-status",
			title: "Plugin status",
			subtitle:
				installedPlugins.length === 0
					? "No plugins installed"
					: erroredPlugins > 0
						? `${erroredPlugins} need attention`
						: `${runningPlugins} of ${installedPlugins.length} running`,
		},
	] satisfies HomeWidget[];
	$: appWidgets = apps.map((app) => ({
		id: `app:${app.id}`,
		kind: "app",
		title: app.name,
		subtitle: app.app.category ?? "App",
		target: app.id,
	})) satisfies HomeWidget[];
	$: pluginWidgets = installedPlugins.map((plugin) => ({
		id: `plugin:${plugin.id}`,
		kind: "plugin",
		title: plugin.name,
		subtitle: plugin.lastError ? "Needs attention" : plugin.enabled ? "Running" : "Stopped",
		target: plugin.id,
	})) satisfies HomeWidget[];
	$: actionWidgets = actions.map((action) => ({
		id: `action:${action.label}`,
		kind: "action",
		title: action.label,
		subtitle: "Action",
		target: action.label,
	})) satisfies HomeWidget[];
	$: availableWidgets = [
		...systemWidgets,
		...actionWidgets,
		...appWidgets,
		...pluginWidgets,
	];
	$: placedWidgets = placedWidgetIds
		.map((id) => availableWidgets.find((widget) => widget.id === id))
		.filter((widget): widget is HomeWidget => Boolean(widget));
	$: unplacedWidgets = availableWidgets.filter((widget) => !placedWidgetIds.includes(widget.id));

	async function refreshWidgets() {
		loading = true;
		error = "";
		try {
			[apps, installedPlugins] = await Promise.all([
				loadApps().catch(() => []),
				plugins.list().catch(() => []),
			]);
			if (placedWidgetIds.length === 0) {
				placedWidgetIds = [
					"apps",
					...actions.map((action) => `action:${action.label}`),
					"plugin-status",
				];
				savePlacedWidgetIds();
			}
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
	}

	function loadPlacedWidgetIds() {
		try {
			const raw = localStorage.getItem(widgetStorageKey);
			const parsed = raw ? JSON.parse(raw) : null;
			return Array.isArray(parsed) ? parsed.filter((id) => typeof id === "string") : [];
		} catch {
			return [];
		}
	}

	function savePlacedWidgetIds() {
		localStorage.setItem(widgetStorageKey, JSON.stringify(placedWidgetIds));
	}

	function addWidget(id: string) {
		if (!id || placedWidgetIds.includes(id)) return;
		placedWidgetIds = [...placedWidgetIds, id];
		savePlacedWidgetIds();
	}

	function removeWidget(id: string) {
		placedWidgetIds = placedWidgetIds.filter((widgetId) => widgetId !== id);
		savePlacedWidgetIds();
	}

	function moveWidgetBefore(sourceId: string, targetId: string) {
		if (!sourceId || !targetId || sourceId === targetId) return;
		const sourceIndex = placedWidgetIds.indexOf(sourceId);
		const targetIndex = placedWidgetIds.indexOf(targetId);
		if (sourceIndex < 0 || targetIndex < 0) return;
		const next = [...placedWidgetIds];
		next.splice(sourceIndex, 1);
		next.splice(next.indexOf(targetId), 0, sourceId);
		placedWidgetIds = next;
	}

	function iconFor(widget: HomeWidget) {
		if (widget.kind === "apps" || widget.kind === "app") return LayoutGridIcon;
		if (widget.kind === "plugin-status" || widget.kind === "plugin") return BlocksIcon;
		return ActivityIcon;
	}

	function runWidget(widget: HomeWidget) {
		if (widget.kind === "app" && widget.target) {
			dispatch("launchApp", { appId: widget.target });
			return;
		}
		if (widget.kind === "plugin" && widget.target) {
			dispatch("pluginSettings", { pluginId: widget.target });
			return;
		}
		if (widget.kind === "action") {
			actions.find((action) => action.label === widget.target)?.run();
			return;
		}
		if (widget.kind === "apps") dispatch("navigate", { section: "apps" });
		if (widget.kind === "plugin-status") dispatch("navigate", { section: "plugins" });
	}

	function appFor(widget: HomeWidget) {
		return apps.find((app) => app.id === widget.target);
	}

	function startDrag(widgetId: string, event: DragEvent) {
		if (!editing) return;
		draggedWidgetId = widgetId;
		event.dataTransfer?.setData("text/plain", widgetId);
		if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
	}

	function dragOver(widgetId: string, event: DragEvent) {
		if (!editing || !draggedWidgetId) return;
		event.preventDefault();
		dragOverWidgetId = widgetId;
		moveWidgetBefore(draggedWidgetId, widgetId);
	}

	function endDrag() {
		if (draggedWidgetId) savePlacedWidgetIds();
		draggedWidgetId = "";
		dragOverWidgetId = "";
	}
</script>

<section class="home-widget-system">
	{#if error}
		<div class="widget-empty">{error}</div>
	{:else if loading && placedWidgets.length === 0}
		<div class="widget-empty">Loading widgets...</div>
	{:else if placedWidgets.length === 0}
		<div class="widget-empty">Add a widget to start.</div>
	{:else}
		<div class="home-widget-grid">
			{#each placedWidgets as widget (widget.id)}
				<article
					class:dragging={draggedWidgetId === widget.id}
					class:drop-target={dragOverWidgetId === widget.id}
					class:editing
					class="home-widget-card {widget.kind}"
					draggable={editing}
					on:dragstart={(event) => startDrag(widget.id, event)}
					on:dragover={(event) => dragOver(widget.id, event)}
					on:dragend={endDrag}
					on:drop={endDrag}
				>
					<button
						class="home-widget-body"
						aria-label={editing ? `Remove ${widget.title}` : widget.title}
						title={editing ? "Tap to remove, drag to reorder" : widget.title}
						on:click={() => (editing ? removeWidget(widget.id) : runWidget(widget))}
					>
						<span class="widget-icon" aria-hidden="true">
							{#if widget.kind === "app" && appFor(widget)}
								{@const app = appFor(widget)}
								{#if app && isImageAppIcon(app.app.icon)}
									<img src={app.app.icon} alt="" />
								{:else if app}
									<span>{fallbackAppIcon(app)}</span>
								{/if}
							{:else}
								<svelte:component this={iconFor(widget)} size={28} strokeWidth={2.2} />
							{/if}
						</span>
						<span class="widget-copy">
							<strong>{widget.title}</strong>
							<small>{widget.subtitle}</small>
						</span>
					</button>
				</article>
			{/each}
		</div>
	{/if}

	{#if editing}
		<div class="widget-tray" aria-label="Available widgets">
			{#each unplacedWidgets as widget (widget.id)}
				<button class="widget-option {widget.kind}" on:click={() => addWidget(widget.id)}>
					<span class="widget-icon" aria-hidden="true">
						{#if widget.kind === "app" && appFor(widget)}
							{@const app = appFor(widget)}
							{#if app && isImageAppIcon(app.app.icon)}
								<img src={app.app.icon} alt="" />
							{:else if app}
								<span>{fallbackAppIcon(app)}</span>
							{/if}
						{:else}
							<svelte:component this={iconFor(widget)} size={24} strokeWidth={2.2} />
						{/if}
					</span>
					<span class="widget-copy">
						<strong>{widget.title}</strong>
						<small>{widget.subtitle}</small>
					</span>
					<PlusIcon size={16} strokeWidth={2.4} />
				</button>
			{/each}
		</div>
	{/if}
</section>
