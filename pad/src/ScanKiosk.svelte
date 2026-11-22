<script lang="ts">
  /**
   * The calibration's scan step: the rear camera, looking for the four
   * markers the kiosk draws on its game while a phone is on this step
   * (input/markers.ts). Once all four hold steady for half a second, their
   * average goes to `onScan`, which works out where the player stands. Any
   * browser with a camera works: no BarcodeDetector needed.
   */
  import { onDestroy, onMount } from 'svelte'
  import { detectScanMarkers, toGray } from './input/markers'
  import type { Pt } from './platform/viewpoint'

  /** Returns why the scan can't be used, or null when it was taken. */
  export let onScan: (corners: Pt[], frame: { w: number; h: number }) => string | null
  export let onSkip: () => void
  /** The latest problem's words, to show under the picture. */
  export let problem = ''

  /** Frames are shrunk to this long side before searching: plenty for markers, a few ms each. */
  const LONG_SIDE = 480
  const EVERY_MS = 90
  /** Consecutive frames the markers must hold still for. */
  const STEADY = 5
  /** How far a corner may move between those frames, as a share of the long side. */
  const STEADY_MOVE = 0.025

  let video: HTMLVideoElement
  let stream: MediaStream | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  let stopped = false
  let error = ''
  let starting = true

  let frame = { w: 0, h: 0 }
  let found: Pt[] | null = null
  let streak: Pt[][] = []
  let pausedUntil = 0
  let boxW = 0
  let boxH = 0

  onMount(async () => {
    try {
      // The camera's own picture, uncropped and unscaled: the angle maths assumes
      // the frame's long side spans the lens's full view (viewpoint.ts).
      stream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: { ideal: 'environment' }, resizeMode: { ideal: 'none' }, width: { ideal: 1280 } } as MediaTrackConstraints,
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
    try {
      look()
    } catch {
      /* a frame that could not be read */
    }
    timer = setTimeout(loop, EVERY_MS)
  }

  let canvas: HTMLCanvasElement | null = null
  let ctx: CanvasRenderingContext2D | null = null
  let gray: Uint8Array | null = null

  function look() {
    const vw = video.videoWidth
    const vh = video.videoHeight
    if (video.readyState < 2 || !vw || !vh) return
    const k = Math.min(1, LONG_SIDE / Math.max(vw, vh))
    const w = Math.round(vw * k)
    const h = Math.round(vh * k)
    if (!canvas || canvas.width !== w || canvas.height !== h) {
      canvas = document.createElement('canvas')
      canvas.width = w
      canvas.height = h
      ctx = canvas.getContext('2d', { willReadFrequently: true })
      gray = new Uint8Array(w * h)
      streak = []
    }
    if (!ctx || !gray) return
    frame = { w, h }
    ctx.drawImage(video, 0, 0, w, h)
    const quad = detectScanMarkers(toGray(ctx.getImageData(0, 0, w, h).data, w, h, gray), w, h)
    found = quad
    if (!quad || performance.now() < pausedUntil) {
      streak = []
      return
    }
    const last = streak[streak.length - 1]
    const tol = STEADY_MOVE * Math.max(w, h)
    if (last && quad.some((p, i) => Math.hypot(p[0] - last[i][0], p[1] - last[i][1]) > tol)) streak = []
    streak = [...streak, quad]
    if (streak.length < STEADY) return
    const avg = [0, 1, 2, 3].map((i) => [
      streak.reduce((s, q) => s + q[i][0], 0) / streak.length,
      streak.reduce((s, q) => s + q[i][1], 0) / streak.length,
    ]) as Pt[]
    streak = []
    if (onScan(avg, { w, h })) pausedUntil = performance.now() + 1200
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
  $: shown = found && cover ? found.map(([x, y]) => [ox + x * cover, oy + y * cover]) : null
  $: progress = streak.length / STEADY
  $: status = error
    ? error
    : problem
      ? problem
      : starting
        ? 'Starting the camera…'
        : found
          ? 'Got it. Hold still…'
          : 'Point the camera at the kiosk until all four white squares are on screen.'
</script>

<div class="scan">
  <!-- The whole frame is shown (its own shape), so every marker found is on screen. -->
  <div class="view" style="--ar: {frame.w && frame.h ? frame.w / frame.h : 0.75}" bind:clientWidth={boxW} bind:clientHeight={boxH}>
    <!-- svelte-ignore a11y-media-has-caption -->
    <video bind:this={video} playsinline muted autoplay></video>
    {#if shown}
      <svg class="marks" viewBox="0 0 {boxW} {boxH}" aria-hidden="true">
        <polygon points={shown.map((p) => p.join(',')).join(' ')} />
        {#each shown as [x, y]}
          <circle cx={x} cy={y} r="6" />
        {/each}
      </svg>
    {:else if !error}
      <div class="corners" aria-hidden="true"><span></span><span></span><span></span><span></span></div>
    {/if}
  </div>
  <p class="status" class:bad={!!error || !!problem} aria-live="polite">{status}</p>
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
    /* At most 38vh tall, at most the full width, in the frame's own shape. */
    width: min(100%, calc(min(38vh, 360px) * var(--ar)));
    aspect-ratio: var(--ar);
    border-radius: var(--yui-radius-card);
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
  .marks polygon {
    fill: rgba(255, 198, 39, 0.18);
    stroke: var(--kit-yellow, #ffc627);
    stroke-width: 3;
    stroke-linejoin: round;
  }
  .marks circle {
    fill: none;
    stroke: var(--kit-yellow, #ffc627);
    stroke-width: 3;
  }
  /* Where the squares should go, until they're found. */
  .corners {
    position: absolute;
    inset: 16% 12%;
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
