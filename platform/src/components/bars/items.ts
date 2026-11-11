/**
 * Every bar item is registered here, one line each. A feature adds its
 * component under components/bars/ and a registerBarItem call below.
 */
import { registerBarItem } from "./registry";
import ExitFullscreen from "./ExitFullscreen.svelte";
import YuiLogo from "./YuiLogo.svelte";
import GameTitle from "./GameTitle.svelte";
import HypeCombo from "./HypeCombo.svelte";
import JoinCorner from "./JoinCorner.svelte";
import PickedBy from "./PickedBy.svelte";
import SeatChips from "./SeatChips.svelte";

registerBarItem({ id: "game-title", bar: "top", zone: "center", order: 0, component: GameTitle });
registerBarItem({ id: "yui-logo", bar: "top", zone: "right", order: 90, component: YuiLogo });
registerBarItem({ id: "exit-fullscreen", bar: "top", zone: "right", order: 100, component: ExitFullscreen });

// Social layer (reactions, seat chips, join corner). Social events come from
// the controller (`pad.reaction`, `pad.poke`, `pad.input`); see social.ts.
registerBarItem({ id: "picked-by", bar: "top", zone: "left", order: 0, component: PickedBy });
registerBarItem({ id: "hype-combo", bar: "top", zone: "fill", order: 0, component: HypeCombo });
registerBarItem({ id: "seat-chips", bar: "bottom", zone: "fill", order: 0, component: SeatChips });
registerBarItem({ id: "join-corner", bar: "bottom", zone: "right", order: 0, component: JoinCorner });
