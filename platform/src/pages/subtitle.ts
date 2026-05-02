import type { RouteDefinition } from "./routes";

export type SubtitleContext = {
    appCount: number;
}

export function resolveSubtitle(route: RouteDefinition, ctx: SubtitleContext): string {
    if (route.id === "apps") {
        return ctx.appCount === 1
            ? "1 app installed"
            : `${ctx.appCount} apps`;
    }
    return route.defaultSubtitle
}