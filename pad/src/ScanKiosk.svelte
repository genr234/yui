<script lang="ts">
  /**
   * The calibration's scan step: the rear camera, reading the QR code the
   * kiosk shows on its game while a phone is on this step, then the four
   * white squares near the game's corners, looked for only where the code
   * says they are (input/aimCodes.ts). Only the kiosk's own code counts: its
   * text says where everything is. Once it holds steady for a few reads,
   * every point found goes to `onScan`, which works out where the player
   * stands. Any browser with a camera works: no BarcodeDetector needed.
   */
  import { onDestroy, onMount } from 'svelte'
  import { findAimCodes, findSquare, regionsToRead, squareRegions, toFrame, type Region, type SeenCode } from './input/aimCodes'
  import type { Pt } from './platform/viewpoint'

  /** Returns why the scan can't be used, or null when it was taken. */
  export let onScan: (plane: Pt[], image: Pt[], frame: { w: number; h: number }) => string | null
  export let onSkip: () => void
  /** The latest problem's words, to show under the picture. */
  export let problem = ''

  /** Reads the code must hold still for. */
  const STEADY = 3
  /** How far a corner may move between those reads, as a share of the frame's long side. */
  const STEADY_MOVE = 0.03
  /** Regions read per frame at most (each ~30–80 ms on a phone). */
  const READS_PER_FRAME = 2
  const MIN_GAP_MS = 60

  let video: HTMLVideoElement
  let stream: MediaStream | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  let stopped = false
  let error = ''
  let starting = true

  let frame = { w: 0, h: 0 }
  let found: SeenCode | null = null
  let lastSeenAt = 0
  let turn = 0
  /** Each read: the code's corners, then each square's centre or null. */
  let streak: (Pt | null)[][] = []
  /** The squares found in the latest read, for the picture. */
  let squaresSeen: Pt[] = []
  let pausedUntil = 0
  /** Just after a refused scan: its problem shows until the next try. */
  let paused = false
  let boxW = 0
  let boxH = 0

  onMount(async () => {
    try {
      // The camera's own picture, uncropped and unscaled: the angle maths assumes the
      // frame's long side spans the lens's full view (viewpoint.ts), and the code is
      // read at full resolution, so ask for a big one.
      stream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: { ideal: 'environment' }, resizeMode: { ideal: 'none' }, width: { ideal: 1920 } } as MediaTrackConstraints,
        audio: false,
      })
      if (stopped) return stop()
      video.srcObject = stream
      await video.play()
      starting = false
      loop()
    } catch {
      starting = false
      error = "The camera isn't available. Allow camera access and try again, or aim at the edges instead."
    }
  })

  function loop() {
    if (stopped) return
    const t0 = performance.now()
    try {
      look()
    } catch {
      /* a frame that could not be read */
    }
    timer = setTimeout(loop, Math.max(MIN_GAP_MS, performance.now() - t0))
  }

  let canvas: HTMLCanvasElement | null = null
  let ctx: CanvasRenderingContext2D | null = null

  /** A region of the current video frame, as RGBA at the region's read size. */
  function pixels(r: Region): Uint8ClampedArray | null {
    if (!canvas || canvas.width !== r.dw || canvas.height !== r.dh) {
      canvas ??= document.createElement('canvas')
      canvas.width = r.dw
      canvas.height = r.dh
      ctx = canvas.getContext('2d', { willReadFrequently: true })
    }
    if (!ctx) return null
    ctx.drawImage(video, r.sx, r.sy, r.sw, r.sh, 0, 0, r.dw, r.dh)
    return ctx.getImageData(0, 0, r.dw, r.dh).data
  }

  function read(r: Region): SeenCode | null {
    const px = pixels(r)
    const codes = px ? findAimCodes(px, r.dw, r.dh) : []
    return codes.length ? toFrame(codes[0], r) : null
  }

  /** Each of the code's squares in this frame (frame pixels), or null where it wasn't found. */
  function squares(code: SeenCode): (Pt | null)[] {
    return squareRegions(code, frame).map((r) => {
      if (!r) return null
      const px = pixels(r)
      if (!px) return null
      const gray = new Uint8Array(r.dw * r.dh)
      for (let i = 0; i < gray.length; i++) gray[i] = (px[i * 4] * 77 + px[i * 4 + 1] * 150 + px[i * 4 + 2] * 29) >> 8
      const c = findSquare(gray, r.dw, r.dh)
      return c ? [r.sx + (c[0] * r.sw) / r.dw, r.sy + (c[1] * r.sh) / r.dh] : null
    })
  }

  function look() {
    const vw = video.videoWidth
    const vh = video.videoHeight
    if (video.readyState < 2 || !vw || !vh) return
    if (vw !== frame.w || vh !== frame.h) {
      frame = { w: vw, h: vh }
      streak = []
    }
    const now = performance.now()
    const recent = found && now - lastSeenAt < 1000 ? found.image : null
    let code: SeenCode | null = null
    for (const r of regionsToRead(frame, recent, turn++).slice(0, READS_PER_FRAME)) {
      code = read(r)
      if (code) break
    }
    if (code) {
      found = code
      lastSeenAt = now
    } else if (now - lastSeenAt > 600) found = null
    paused = now < pausedUntil
    if (!code || paused) {
      streak = []
      squaresSeen = []
      return
    }
    const sq = squares(code)
    squaresSeen = sq.filter((p): p is Pt => !!p)
    const last = streak[streak.length - 1]
    const tol = STEADY_MOVE * Math.max(vw, vh)
    const moved = (p: Pt, i: number) => {
      const q = last?.[i]
      return !!q && Math.hypot(p[0] - q[0], p[1] - q[1]) > tol
    }
    if (last && code.image.some(moved)) streak = []
    streak = [...streak, [...code.image, ...sq]]
    if (streak.length < STEADY) return
    // Every point seen in most of the reads, averaged over them: the code's
    // corners always, a square when it was found at least twice.
    const plane: Pt[] = []
    const image: Pt[] = []
    const where = [...code.plane, ...code.squares]
    for (let i = 0; i < where.length; i++) {
      const seen = streak.map((r) => r[i]).filter((p): p is Pt => !!p)
      if (seen.length < Math.min(2, streak.length)) continue
      plane.push(where[i])
      image.push([seen.reduce((s, p) => s + p[0], 0) / seen.length, seen.reduce((s, p) => s + p[1], 0) / seen.length])
    }
    streak = []
    if (onScan(plane, image, frame)) {
      pausedUntil = performance.now() + 1200
      paused = true
    }
  }

  function stop() {
    stopped = true
    clearTimeout(timer)
    stream?.getTracks().forEach((t) => t.stop())
    stream = null
  }

  onDestroy(stop)

  // Frame pixels → the box, which shows the video with object-fit: cover.
  $: cover = frame.w && boxW ? Math.max(boxW / frame.w, boxH / frame.h) : 0
  $: ox = (boxW - frame.w * cover) / 2
  $: oy = (boxH - frame.h * cover) / 2
  $: shown = found && cover ? found.image.map(([x, y]) => [ox + x * cover, oy + y * cover]) : null
  $: dots = cover ? squaresSeen.map(([x, y]) => [ox + x * cover, oy + y * cover]) : []
  $: progress = streak.length / STEADY
  // A refused scan's problem shows while it waits to try again, or while nothing is found;
  // once the code is held steady again, the progress speaks instead.
  $: shownProblem = problem && (paused || !found) ? problem : ''
  $: status = error
    ? error
    : shownProblem
      ? shownProblem
      : starting
        ? 'Starting the camera…'
        : found
          ? found.squares.length && squaresSeen.length < found.squares.length
            ? 'Got the code. Hold still, with the four white squares on screen too.'
            : 'Got it. Hold still…'
          : 'Point the camera at the code on the kiosk.'
</script>

<div class="scan">
  <!-- The whole frame is shown (its own shape), so the code is outlined wherever it is found. -->
  <div class="view" style="--ar: {frame.w && frame.h ? frame.w / frame.h : 0.75}" bind:clientWidth={boxW} bind:clientHeight={boxH}>
    <!-- svelte-ignore a11y-media-has-caption -->
    <video bind:this={video} playsinline muted autoplay></video>
    {#if shown}
      <svg class="marks" viewBox="0 0 {boxW} {boxH}" aria-hidden="true">
        <polygon points={shown.map((p) => p.join(',')).join(' ')} />
        {#each dots as [x, y]}<circle cx={x} cy={y} r="7" />{/each}
      </svg>
    {:else if !error}
      <div class="corners" aria-hidden="true"><span></span><span></span><span></span><span></span></div>
    {/if}
  </div>
  <p class="status" class:bad={!!error || !!shownProblem} aria-live="polite">{status}</p>
  <div class="meter" style="--p: {progress}"><span></span></div>
  <button class={error ? 'primary' : 'link'} type="button" on:click={onSkip}>
    {error ? 'Aim at the edges instead' : "Can't scan? Aim at the edges instead"}
  </button>
</div>

<style>
  .scan {
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
  }
  .view {
    position: relative;
    /* At most 32vh tall, at most the full width, in the frame's own shape. */
    width: min(100%, calc(min(32vh, 340px) * var(--ar)));
    aspect-ratio: var(--ar);
    /* A viewfinder in the kit's panel rim, sitting on its lip like the cards. */
    border: 6px solid var(--kit-panel);
    border-radius: var(--yui-radius-shell);
    box-shadow:
      0 0 0 2px var(--kit-panel-line),
      0 calc(var(--yui-lip) + 2px) 0 var(--kit-panel-lip),
      0 14px 30px rgba(10, 50, 90, 0.25);
    overflow: hidden;
    background: #000;
  }
  video {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .marks {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
  }
  .marks circle {
    fill: none;
    stroke: var(--kit-yellow, #ffc627);
    stroke-width: 3;
  }
  .marks polygon {
    fill: rgba(255, 198, 39, 0.18);
    stroke: var(--kit-yellow, #ffc627);
    stroke-width: 3;
    stroke-linejoin: round;
  }
  /* The middle of the picture, read sharpest: where to put the code until it's found. */
  .corners {
    position: absolute;
    inset: 22.5%;
  }
  .corners span {
    position: absolute;
    width: 22px;
    height: 22px;
    border: 4px solid rgba(255, 255, 255, 0.85);
    border-radius: 4px;
    animation: hunt 1.6s ease-in-out infinite alternate;
  }
  .corners span:nth-child(1) {
    left: 0;
    top: 0;
  }
  .corners span:nth-child(2) {
    right: 0;
    top: 0;
  }
  .corners span:nth-child(3) {
    right: 0;
    bottom: 0;
  }
  .corners span:nth-child(4) {
    left: 0;
    bottom: 0;
  }
  @keyframes hunt {
    from {
      opacity: 0.35;
    }
    to {
      opacity: 1;
    }
  }
  .status {
    margin: 0;
    min-height: 2.6em;
    font-weight: 800;
  }
  .status.bad {
    color: var(--kit-yellow);
  }
  .meter {
    width: 100%;
    height: 14px;
    border-radius: 999px;
    background: var(--yui-chrome-fill);
    overflow: hidden;
  }
  .meter span {
    display: block;
    height: 100%;
    width: calc(var(--p) * 100%);
    background: var(--kit-green);
    transition: width 80ms linear;
  }
  .link {
    border: 0;
    background: none;
    color: var(--fg);
    font: inherit;
    font-weight: 800;
    text-decoration: underline;
    padding: 8px;
    min-height: 44px;
  }
  .primary {
    width: 100%;
    max-width: 320px;
  }
  @media (prefers-reduced-motion: reduce) {
    .corners span {
      animation: none;
    }
  }
</style>
