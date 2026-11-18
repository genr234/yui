<script lang="ts">
  import { onDestroy } from 'svelte'
  import { capture, usePad } from './context'
  import { SurfaceTouches, contentRect } from './surface'
  import { surfaceState, surfaceStream } from '../platform/surface'

  export let props: Record<string, unknown>
  export let style = ''

  const pad = usePad()

  $: surfaceId = typeof props.surface === 'string' ? props.surface : ''
  $: touchOn = props.touch === true

  // The video shows only while the kiosk says this surface is on this phone.
  $: mine = $surfaceState && $surfaceState.id === surfaceId ? $surfaceState : null
  $: live = mine?.state === 'on' && !!$surfaceStream
  $: elsewhere = mine?.state === 'off'

  let box: HTMLDivElement
  let video: HTMLVideoElement
  let videoW = 0
  let videoH = 0
  let playing = false

  $: if (video) bind(video, live ? $surfaceStream : null)
  function bind(el: HTMLVideoElement, stream: MediaStream | null) {
    if (el.srcObject !== stream) el.srcObject = stream
    if (stream) el.play().catch(() => {})
    else playing = false
  }

  const touches = new SurfaceTouches()

  function rect() {
    const r = box.getBoundingClientRect()
    return {
      r,
      content: contentRect(r.width, r.height, videoW || mine?.width || 0, videoH || mine?.height || 0),
    }
  }
  function report() {
    pad.touch(touches.points())
  }
  function down(e: PointerEvent) {
    if (!touchOn || !live) return
    const { r, content } = rect()
    if (touches.down(e.pointerId, content, e.clientX - r.left, e.clientY - r.top)) {
      capture(e)
      report()
    }
  }
  function move(e: PointerEvent) {
    if (!touches.count) return
    const { r, content } = rect()
    if (touches.move(e.pointerId, content, e.clientX - r.left, e.clientY - r.top)) report()
  }
  function up(e: PointerEvent) {
    if (touches.up(e.pointerId)) report()
  }

  // Fingers lift when the picture goes away.
  $: if (!live && touches.count) {
    touches.clear()
    report()
  }

  onDestroy(() => {
    if (touches.count) {
      touches.clear()
      report()
    }
  })
</script>

<div
  bind:this={box}
  class="surface"
  class:touch={touchOn && live}
  {style}
  role="group"
  aria-label="Second screen"
  on:pointerdown|preventDefault={down}
  on:pointermove={move}
  on:pointerup={up}
  on:pointercancel={up}
  on:lostpointercapture={up}
>
  <video
    bind:this={video}
    muted
    playsinline
    autoplay
    disablepictureinpicture
    class:shown={live && playing}
    on:loadedmetadata={() => {
      videoW = video.videoWidth
      videoH = video.videoHeight
    }}
    on:playing={() => (playing = true)}
    on:emptied={() => (playing = false)}
  ></video>
  {#if !(live && playing)}
    <p class="note">{elsewhere ? 'This screen is on another phone' : 'Connecting…'}</p>
  {/if}
</div>

<style>
  .surface {
    position: relative;
    flex: 1 1 auto;
    min-width: 0;
    min-height: 0;
    display: grid;
    place-items: center;
    overflow: hidden;
    border: 2px solid var(--kit-panel-line);
    border-radius: var(--yui-radius-shell);
    background: #000;
    touch-action: none;
  }
  video {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: contain;
    background: #000;
    visibility: hidden;
    pointer-events: none;
  }
  video.shown {
    visibility: visible;
  }
  .note {
    margin: 0;
    padding: 12px;
    color: #fff;
    font-weight: 800;
    text-align: center;
    opacity: 0.8;
  }
</style>
