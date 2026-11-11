/**
 * Every bar item is registered here, one line each. A feature adds its
 * component under components/bars/ and a registerBarItem call below.
 */
import { registerBarItem } from "./registry";
import ExitFullscreen from "./ExitFullscreen.svelte";
import GameTitle from "./GameTitle.svelte";

registerBarItem({ id: "game-title", bar: "top", zone: "center", order: 0, component: GameTitle });
registerBarItem({ id: "exit-fullscreen", bar: "top", zone: "right", order: 100, component: ExitFullscreen });
