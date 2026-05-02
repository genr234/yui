<script lang="ts">
	import type { ActionItem, DetailItem, Section } from "../types";
	import { findRoute } from "./routes";

	export let section: Section = "home";
	export let homeActions: ActionItem[] = [];
	export let toolActions: ActionItem[] = [];
	export let settingDetails: DetailItem[] = [];
	export let diagnostics = "";

    function getRouteProps(section: Section) {
        if (section === "home") {
            return { actions: homeActions };
        } else if (section === "tools") {
            return { actions: toolActions, diagnostics };
        } else if (section === "settings") {
            return { details: settingDetails };
        } else {
            return {};
        }
    }

	$: route = findRoute(section);
	$: routeProps = getRouteProps(section)
</script>

<svelte:component this={route.component} {...routeProps} />
