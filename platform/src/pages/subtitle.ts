import { loadApps } from "../apps";
import type { RouteDefinition } from "./routes";

export type SubtitleContext = {
}

const apps = await loadApps().then(apps => apps.length).catch(() => 0);

export function resolveSubtitle(route: RouteDefinition, ctx: SubtitleContext): string {
    if (route.id === "apps") {
        return apps === 1
            ? "1 app installed"
            : `${apps} apps`;
    }
    return route.defaultSubtitle
}