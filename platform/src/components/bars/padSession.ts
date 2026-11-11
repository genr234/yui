import { pad, type PadState } from "../../sdk/pad";

/** Calls `fn` with the current play session now and on every change; returns the unsubscribe. */
export function watchPadState(fn: (state: PadState) => void): () => void {
	let live = true;
	const off = pad.onState((state) => live && fn(state));
	void pad.state().then((state) => live && fn(state));
	return () => {
		live = false;
		off();
	};
}
