import type { ComponentType } from "svelte";

export type Section = string;

export type BridgeState = "connecting" | "online" | "offline" | string;

export type SectionItem = {
	id: Section;
	label: string;
	subtitle?: string;
	icon: ComponentType;
};

export type MetricItem = {
	label: string;
	value: string;
	help: string;
	icon: string;
};

export type ActionTone = "blue" | "green" | "violet" | "orange";

export type ActionItem = {
	label: string;
	icon: ComponentType;
	tone: ActionTone;
	run: () => void | Promise<void>;
};

export type DetailItem = {
	label: string;
	value: string;
};

export type SidebarThemeImage = {
	id: string;
	name: string;
	src: string;
};

export type SidebarThemeSettings = {
	enabled: boolean;
	images: SidebarThemeImage[];
};
