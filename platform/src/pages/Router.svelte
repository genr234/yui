<script lang="ts">
	import { createEventDispatcher } from "svelte";
	import type { ActionItem, DetailItem, Section } from "../types";
	import About from "./About.svelte";
	import Apps from "./Apps.svelte";
	import Home from "./Home.svelte";
	import Settings from "./Settings.svelte";
	import Tools from "./Tools.svelte";

	export let section: Section = "home";
	export let homeActions: ActionItem[] = [];
	export let toolActions: ActionItem[] = [];
	export let settingDetails: DetailItem[] = [];
	export let diagnostics = "";
	const dispatch = createEventDispatcher<{ appLaunched: { active: boolean } }>();
</script>

<div class="router-panel" hidden={section !== "home"} aria-hidden={section !== "home"}>
	<Home actions={homeActions} />
</div>

<div class="router-panel" hidden={section !== "apps"} aria-hidden={section !== "apps"}>
	<Apps on:launched={(event) => dispatch("appLaunched", event.detail)} />
</div>

<div class="router-panel" hidden={section !== "tools"} aria-hidden={section !== "tools"}>
	<Tools actions={toolActions} {diagnostics} />
</div>

<div class="router-panel" hidden={section !== "settings"} aria-hidden={section !== "settings"}>
	<Settings details={settingDetails} />
</div>

<div class="router-panel" hidden={section !== "about"} aria-hidden={section !== "about"}>
	<About />
</div>
