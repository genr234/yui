import type { RouteDefinition } from "./routes";

export type SubtitleContext = {
	appCount?: number | null;
	pluginCount?: number | null;
};

export function resolveSubtitle(route: RouteDefinition, ctx: SubtitleContext): string {
	if (route.id === "apps" && typeof ctx.appCount === "number") {
		return ctx.appCount === 1 ? "1 app installed" : `${ctx.appCount} apps installed`;
	}
	if (route.id === "plugins" && typeof ctx.pluginCount === "number") {
		return ctx.pluginCount === 1
			? "1 plugin installed"
			: `${ctx.pluginCount} plugins installed`;
	}
	return route.defaultSubtitle;
}
