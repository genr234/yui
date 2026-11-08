/**
 * Short synthesized chimes for phones joining and leaving (no audio assets).
 * Join rises, leave falls. Quiet by design: the kiosk may be in a shared room.
 */
let context: AudioContext | null = null;

function audio(): AudioContext | null {
	if (typeof window === "undefined") return null;
	const Ctor = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
	if (!Ctor) return null;
	try {
		context ??= new Ctor();
		if (context.state === "suspended") void context.resume().catch(() => undefined);
		return context;
	} catch {
		return null;
	}
}

/** Note frequencies (Hz) for each chime. */
export const chimeNotes = {
	join: [659.25, 987.77],
	leave: [783.99, 523.25],
} as const;

export function playChime(kind: keyof typeof chimeNotes, gain = 0.12) {
	const ctx = audio();
	if (!ctx) return;
	const start = ctx.currentTime + 0.01;
	chimeNotes[kind].forEach((freq, index) => {
		const at = start + index * 0.11;
		const osc = ctx.createOscillator();
		const env = ctx.createGain();
		osc.type = "sine";
		osc.frequency.setValueAtTime(freq, at);
		env.gain.setValueAtTime(0, at);
		env.gain.linearRampToValueAtTime(gain, at + 0.012);
		env.gain.exponentialRampToValueAtTime(0.0001, at + 0.32);
		osc.connect(env).connect(ctx.destination);
		osc.start(at);
		osc.stop(at + 0.34);
	});
}
