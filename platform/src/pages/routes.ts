import type { ComponentType } from "svelte";
import HouseIcon from "lucide-svelte/icons/house";
import InfoIcon from "lucide-svelte/icons/info";
import LayoutGridIcon from "lucide-svelte/icons/layout-grid";
import SettingsIcon from "lucide-svelte/icons/settings";
import WrenchIcon from "lucide-svelte/icons/wrench";
import About from "./About.svelte";
import Apps from "./Apps.svelte";
import Home from "./Home.svelte";
import Settings from "./Settings.svelte";
import Tools from "./Tools.svelte";
import type { Section, SectionItem } from "../types";

export type RouteDefinition = SectionItem & {
	component: ComponentType;
	defaultSubtitle: string;
};

export const routes: RouteDefinition[] = [
	{
		id: "home",
		label: "Home",
		defaultSubtitle: "System overview and quick actions",
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
		id: "tools",
		label: "Tools",
		defaultSubtitle: "Controller-backed maintenance actions",
		icon: WrenchIcon,
		component: Tools,
	},
	{
		id: "settings",
		label: "Settings",
		defaultSubtitle: "Runtime paths and platform configuration",
		icon: SettingsIcon,
		component: Settings,
	},
	{
		id: "about",
		label: "About",
		defaultSubtitle: "Project notes and attribution",
		icon: InfoIcon,
		component: About,
	},
];

export function findRoute(section: Section): RouteDefinition {
	return routes.find((route) => route.id === section) ?? routes[0];
}
