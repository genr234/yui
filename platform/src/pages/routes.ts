import type { ComponentType } from "svelte";
import HouseIcon from "lucide-svelte/icons/house";
import InfoIcon from "lucide-svelte/icons/info";
import LayoutGridIcon from "lucide-svelte/icons/layout-grid";
import SettingsIcon from "lucide-svelte/icons/settings";
import BlocksIcon from "lucide-svelte/icons/blocks";
import About from "./About.svelte";
import Apps from "./Apps.svelte";
import Home from "./Home.svelte";
import Settings from "./Settings.svelte";
import type { Section, SectionItem } from "../types";
import Plugins from "./Plugins.svelte";

export type RouteDefinition = SectionItem & {
	component: ComponentType;
	defaultSubtitle: string;
};

export const routes: RouteDefinition[] = [
	{
		id: "home",
		label: "Home",
		defaultSubtitle: "",
		icon: HouseIcon,
		component: Home,
	},
	{
		id: "apps",
		label: "Apps",
		defaultSubtitle: "No apps installed... yet...",
		icon: LayoutGridIcon,
		component: Apps,
	},
	{
		id: "plugins",
		label: "Plugins",
		defaultSubtitle: "No plugins installed... yet...",
		icon: BlocksIcon,
		component: Plugins,
	},
	{
		id: "settings",
		label: "Settings",
		defaultSubtitle: "",
		icon: SettingsIcon,
		component: Settings,
	},
	{
		id: "about",
		label: "About",
		defaultSubtitle: "",
		icon: InfoIcon,
		component: About,
	},
];

export function findRoute(section: Section): RouteDefinition {
	return routes.find((route) => route.id === section) ?? routes[0];
}
