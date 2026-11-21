import type { YuiCameraFace } from "./types";

const reacquireMs = 850;

/** Keep following the same position and scale when other faces enter the frame. */
export function createFaceLock() {
	let previous: YuiCameraFace | null = null;
	let seenAt = 0;

	return (faces: YuiCameraFace[], now: number): YuiCameraFace | null => {
		let selected: YuiCameraFace | null = null;
		if (previous && now - seenAt < reacquireMs) {
			let best = 0.7;
			for (const face of faces) {
				const dx = (face.x - previous.x) / previous.width;
				const dy = (face.y - previous.y) / previous.height;
				const scale = Math.abs(Math.log(face.width / previous.width));
				const distance = Math.hypot(dx, dy, 2 * scale);
				if (distance >= best) continue;
				best = distance;
				selected = face;
			}
		} else if (faces.length === 1) {
			selected = faces[0];
		}
		if (selected) {
			previous = selected;
			seenAt = now;
		}
		return selected;
	};
}
