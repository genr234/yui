<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import {
    ARM_COUNT,
    BODY_COUNT,
    BROW_COUNT,
    EYE_RADIUS_MAX,
    EYE_RADIUS_MIN,
    EYE_STYLE_COUNT,
    LEG_COUNT,
    MAX_EYES,
    MOUTH_COUNT,
    NOSE_COUNT,
    TOP_COUNT,
    defaultEyes,
    drawEye,
    encodeCreature,
    joltAvatars,
    joltPupil,
    makePupil,
    pointToBody,
    randomCreature,
    stepPupil,
    withBody,
    type Creature,
    type Pupil,
  } from '@yui/avatar'
  import Avatar from './Avatar.svelte'
  import {
    CROP_ZOOM_MAX,
    SELFIE_SIZE,
    cameraErrorMessage,
    cameraSupported,
    capturePhoto,
    captureSelfie,
    cropSquare,
    creatureOf,
    loadImageFile,
    openFrontCamera,
    photoOf,
    squareCrop,
    stopStream,
    withSelfie,
    type SquareCrop,
  } from './avatar'
  import { SEARCH_SIZE, faceSpots, findFace, loadFaceFinder, toGray, type FaceSpots } from './face/face'
  import type { Classifier } from './face/pico'

  /** The avatar being edited: a creature, a plain photo, or anything else (starts a random creature). */
  export let value: string | null = null
  /** The player's colour as a CSS colour. */
  export let color: string
  export let busy = false
  export let error = ''
  export let doneLabel = 'Done'
  export let onDone: (avatar: string) => void
  export let onCancel: () => void

  // Two kinds of avatar, each kept while the player flips between them so
  // nothing is lost: a creature built from parts, or just a round photo.
  type Mode = 'creature' | 'photo'
  const startPhoto = photoOf(value)
  let mode: Mode = startPhoto ? 'photo' : 'creature'
  let creature: Creature = creatureOf(value) ?? randomCreature()
  let photo: string | null = startPhoto ?? creature.photo
  $: encoded = encodeCreature(creature)
  $: result = mode === 'photo' ? photo : encoded

  function setMode(next: Mode) {
    if (next === mode) return
    mode = next
    note = ''
  }

  // ---- parts ----------------------------------------------------------------

  type Part = 'body' | 'eyeStyle' | 'mouth' | 'nose' | 'brow' | 'top' | 'arms' | 'legs'
  const TABS: { part: Part; label: string; count: number; view: 'face' | 'full' }[] = [
    { part: 'body', label: 'Body', count: BODY_COUNT, view: 'face' },
    { part: 'eyeStyle', label: 'Eyes', count: EYE_STYLE_COUNT, view: 'face' },
    { part: 'mouth', label: 'Mouth', count: MOUTH_COUNT, view: 'face' },
    { part: 'top', label: 'Head', count: TOP_COUNT, view: 'face' },
    { part: 'nose', label: 'Nose', count: NOSE_COUNT, view: 'face' },
    { part: 'brow', label: 'Brows', count: BROW_COUNT, view: 'face' },
    { part: 'arms', label: 'Arms', count: ARM_COUNT, view: 'full' },
    { part: 'legs', label: 'Legs', count: LEG_COUNT, view: 'full' },
  ]
  let tab = TABS[0]

  const withPart = (c: Creature, part: Part, i: number): Creature => (part === 'body' ? withBody(c, i) : { ...c, [part]: i })
  $: options = Array.from({ length: tab.count }, (_, i) => ({ i, avatar: encodeCreature(withPart(creature, tab.part, i)) }))

  /** A little shake so every change is felt in the eyes. */
  function wobble() {
    joltAvatars((Math.random() - 0.5) * 6, -3 - Math.random() * 3)
  }

  // One step back, for the dice and anything else that changes a lot at once.
  let history: Creature[] = []
  function change(next: Creature) {
    history = [...history.slice(-19), creature]
    creature = next
    wobble()
  }
  function undo() {
    const last = history[history.length - 1]
    if (!last) return
    history = history.slice(0, -1)
    creature = last
    wobble()
  }

  function choose(i: number) {
    if (creature[tab.part] !== i) change(withPart(creature, tab.part, i))
  }

  function setEyeCount(count: number) {
    const n = Math.min(MAX_EYES, Math.max(1, count))
    if (!creature.photo) change({ ...creature, eyes: defaultEyes(creature.body, n) })
    else if (n > creature.eyes.length) {
      const like = creature.eyes[0] ?? { x: 50, y: 30, r: 12 }
      change({ ...creature, eyes: [...creature.eyes, { x: 50, y: Math.max(8, like.y - like.r * 2), r: like.r }] })
    } else change({ ...creature, eyes: creature.eyes.slice(0, n) })
  }

  function setEyeSize(r: number) {
    creature = { ...creature, eyes: creature.eyes.map((e) => ({ ...e, r })) }
  }

  /** New parts all round, but a selfie and the face on it stay put. */
  function surprise() {
    const next = randomCreature()
    change(
      creature.photo
        ? { ...next, body: creature.body, photo: creature.photo, eyes: creature.eyes, mouthAt: creature.mouthAt, noseAt: creature.noseAt }
        : next,
    )
  }

  // ---- dragging the eyes, mouth and nose on the stage -------------------------

  let stage: HTMLDivElement | undefined
  let stageSize = 220
  type Grab = { kind: 'eye'; index: number } | { kind: 'mouth' } | { kind: 'nose' }
  let grab: (Grab & { dx: number; dy: number }) | null = null

  function bodyPoint(e: PointerEvent) {
    const box = stage!.getBoundingClientRect()
    return pointToBody(creature, 'full', box.width, e.clientX - box.left, e.clientY - box.top)
  }

  function onStageDown(e: PointerEvent) {
    if (!stage || busy) return
    const p = bodyPoint(e)
    const spots: (Grab & { x: number; y: number; reach: number })[] = creature.eyes.map((eye, index) => ({
      kind: 'eye',
      index,
      x: eye.x,
      y: eye.y,
      reach: eye.r + 8,
    }))
    if (creature.mouth) spots.push({ kind: 'mouth', ...creature.mouthAt, reach: 16 })
    if (creature.nose) spots.push({ kind: 'nose', ...creature.noseAt, reach: 12 })
    let best: (typeof spots)[number] | null = null
    let bestDist = Infinity
    for (const s of spots) {
      const dist = Math.hypot(s.x - p.x, s.y - p.y)
      if (dist <= s.reach && dist < bestDist) {
        best = s
        bestDist = dist
      }
    }
    if (!best) return
    history = [...history.slice(-19), creature]
    grab = { ...best, dx: best.x - p.x, dy: best.y - p.y }
    stage.setPointerCapture(e.pointerId)
    e.preventDefault()
  }

  function onStageMove(e: PointerEvent) {
    if (!grab) return
    const p = bodyPoint(e)
    const at = { x: Math.min(100, Math.max(0, p.x + grab.dx)), y: Math.min(100, Math.max(0, p.y + grab.dy)) }
    if (grab.kind === 'eye') {
      const index = grab.index
      creature = { ...creature, eyes: creature.eyes.map((eye, i) => (i === index ? { ...eye, ...at } : eye)) }
    } else if (grab.kind === 'mouth') creature = { ...creature, mouthAt: at }
    else creature = { ...creature, noseAt: at }
  }

  function onStageUp() {
    if (grab) wobble()
    grab = null
  }

  // ---- finding a face ---------------------------------------------------------

  let classify: Classifier | null = null
  const search = typeof document === 'undefined' ? null : document.createElement('canvas')

  /** Starts loading the detector; without it photos still work and the eyes are dragged by hand. */
  function wantFaceFinder(): Promise<Classifier | null> {
    return loadFaceFinder()
      .then((c) => (classify = c))
      .catch(() => null)
  }

  /** Looks for a face in the `crop` square of `source`. Returns spots as shares of that square. */
  function spotsIn(source: CanvasImageSource, crop: SquareCrop, mirror: boolean): FaceSpots | null {
    if (!classify || !search || !(crop.size > 0)) return null
    search.width = SEARCH_SIZE
    search.height = SEARCH_SIZE
    const ctx = search.getContext('2d', { willReadFrequently: true })
    if (!ctx) return null
    ctx.save()
    if (mirror) {
      ctx.translate(SEARCH_SIZE, 0)
      ctx.scale(-1, 1)
    }
    ctx.drawImage(source, crop.sx, crop.sy, crop.size, crop.size, 0, 0, SEARCH_SIZE, SEARCH_SIZE)
    ctx.restore()
    const face = findFace(toGray(ctx.getImageData(0, 0, SEARCH_SIZE, SEARCH_SIZE).data, SEARCH_SIZE, SEARCH_SIZE), classify)
    return face ? faceSpots(face) : null
  }

  /** Puts a photo on the creature, with its eyes, nose and mouth on the face when one is found. */
  function faceOnCreature(url: string, spots: FaceSpots | null) {
    change(withSelfie(creature, url, spots))
    mode = 'creature'
    note = spots ? 'Drag the eyes and mouth to fine-tune.' : "Couldn't find your eyes. Drag them into place."
  }

  /** Photo mode's "put it on a creature": finds the face in the saved photo itself. */
  async function photoToCreature() {
    if (!photo) return
    const url = photo
    await wantFaceFinder()
    const img = new Image()
    img.src = url
    await img.decode().catch(() => {})
    faceOnCreature(url, img.naturalWidth ? spotsIn(img, squareCrop(img.naturalWidth, img.naturalHeight), false) : null)
  }

  // ---- camera -----------------------------------------------------------------

  const canSelfie = cameraSupported()
  /** What the next photo is for: the round photo avatar, or the creature's body. */
  let target: Mode = 'creature'
  let stream: MediaStream | null = null
  let video: HTMLVideoElement | undefined
  let overlay: HTMLCanvasElement | undefined
  let starting = false
  let note = ''
  let spots: FaceSpots | null = null
  let spotsAt = 0
  let pupils: Pupil[] = [makePupil(0), makePupil(1)]
  let raf = 0
  let lastSearch = 0
  let lastFrame = 0

  async function startCamera(forMode: Mode) {
    target = forMode
    note = ''
    starting = true
    spots = null
    if (forMode === 'creature') void wantFaceFinder()
    try {
      const s = await openFrontCamera()
      if (!starting) {
        stopStream(s) // cancelled while the prompt was up
        return
      }
      stream = s
      await tick()
      if (video) {
        video.srcObject = s
        await video.play().catch(() => {})
      }
      lastFrame = 0
      if (forMode === 'creature') raf = requestAnimationFrame(viewfinder)
    } catch (e) {
      note = cameraErrorMessage(e)
      closeCamera()
    } finally {
      starting = false
    }
  }

  function searchFrame(): FaceSpots | null {
    if (!video?.videoWidth) return null
    return spotsIn(video, squareCrop(video.videoWidth, video.videoHeight), true)
  }

  /** Live googly eyes over the camera: they ride on the face and swing as it moves. */
  function viewfinder(now: number) {
    raf = requestAnimationFrame(viewfinder)
    const dt = lastFrame ? Math.min(0.05, (now - lastFrame) / 1000) : 1 / 60
    lastFrame = now
    if (now - lastSearch > 110) {
      lastSearch = now
      const found = searchFrame()
      if (found) {
        if (spots) {
          // Smooth the box, and let the pupils lag behind the head's movement.
          const k = 0.55
          found.eyes.forEach((eye, i) => {
            const old = spots!.eyes[i]
            joltPupil(pupils[i], ((old.x - eye.x) / eye.r) * 9, ((old.y - eye.y) / eye.r) * 9)
            eye.x = old.x + (eye.x - old.x) * k
            eye.y = old.y + (eye.y - old.y) * k
            eye.r = old.r + (eye.r - old.r) * k
          })
        }
        spots = found
        spotsAt = now
      } else if (now - spotsAt > 900) spots = null
    }
    const ctx = overlay?.getContext('2d')
    if (!overlay || !ctx) return
    const box = overlay.getBoundingClientRect()
    const px = Math.round(box.width * Math.min(3, devicePixelRatio || 1))
    if (overlay.width !== px) {
      overlay.width = px
      overlay.height = px
    }
    ctx.clearRect(0, 0, px, px)
    if (!spots) return
    spots.eyes.forEach((eye, i) => {
      stepPupil(pupils[i], dt, 0, 1)
      drawEye(ctx, null, { cx: eye.x * px, cy: eye.y * px, r: eye.r * px }, creature.eyeStyle < 6 ? creature.eyeStyle : 0, pupils[i])
    })
  }

  function capture() {
    const url = video ? captureSelfie(video) : null
    if (!url) {
      note = "Couldn't take the photo. Try again."
      closeCamera()
      return
    }
    if (target === 'photo') {
      photo = url
      note = ''
    } else faceOnCreature(url, searchFrame() ?? (performance.now() - spotsAt < 900 ? spots : null))
    closeCamera()
    wobble()
  }

  function closeCamera() {
    starting = false
    cancelAnimationFrame(raf)
    stopStream(stream)
    stream = null
    if (video) video.srcObject = null
  }

  function removeCreaturePhoto() {
    change({ ...creature, photo: null })
    note = ''
  }

  onDestroy(closeCamera)

  // ---- picking a photo from the gallery, then framing it ------------------------

  let fileInput: HTMLInputElement | undefined
  let crop: { img: HTMLImageElement; w: number; h: number; zoom: number; cx: number; cy: number } | null = null
  let cropBox: HTMLDivElement | undefined
  let cropSize = 260
  let pan: { id: number; x: number; y: number } | null = null

  function pickPhoto(forMode: Mode) {
    target = forMode
    note = ''
    if (fileInput) {
      fileInput.value = ''
      fileInput.click()
    }
  }

  async function onFile() {
    const file = fileInput?.files?.[0]
    if (!file) return
    try {
      const img = await loadImageFile(file)
      const w = img.naturalWidth
      const h = img.naturalHeight
      crop = { img, w, h, zoom: 1, cx: w / 2, cy: h / 2 }
      if (target === 'creature') void wantFaceFinder()
    } catch {
      note = "Couldn't open that picture. Try another one."
    }
  }

  $: shown = crop ? cropSquare(crop.w, crop.h, crop.zoom, crop.cx, crop.cy) : null
  $: scale = shown && shown.size ? cropSize / shown.size : 1

  function setZoom(zoom: number) {
    if (!crop || !shown) return
    // Zoom about the middle of what is shown, not where the centre was asked to be.
    crop = { ...crop, zoom, cx: shown.sx + shown.size / 2, cy: shown.sy + shown.size / 2 }
  }

  function onCropDown(e: PointerEvent) {
    if (!cropBox || pan) return
    pan = { id: e.pointerId, x: e.clientX, y: e.clientY }
    cropBox.setPointerCapture(e.pointerId)
    e.preventDefault()
  }

  function onCropMove(e: PointerEvent) {
    if (!crop || !shown || !pan || e.pointerId !== pan.id) return
    const dx = (e.clientX - pan.x) / scale
    const dy = (e.clientY - pan.y) / scale
    pan = { ...pan, x: e.clientX, y: e.clientY }
    crop = { ...crop, cx: shown.sx + shown.size / 2 - dx, cy: shown.sy + shown.size / 2 - dy }
  }

  function onCropUp(e: PointerEvent) {
    if (pan?.id === e.pointerId) pan = null
  }

  function onCropWheel(e: WheelEvent) {
    if (!crop) return
    e.preventDefault()
    setZoom(Math.min(CROP_ZOOM_MAX, Math.max(1, crop.zoom * Math.exp(-e.deltaY / 400))))
  }

  /** Shows the already-loaded picture itself (its file URL is gone by now). */
  function place(node: HTMLElement, img: HTMLImageElement) {
    img.alt = ''
    img.draggable = false
    node.replaceChildren(img)
    return {
      update(next: HTMLImageElement) {
        if (next !== img) node.replaceChildren((img = next))
      },
    }
  }

  function useCrop() {
    if (!crop || !shown) return
    const url = capturePhoto(crop.img, shown, false)
    if (!url) {
      note = "Couldn't use that picture. Try another one."
      crop = null
      return
    }
    if (target === 'photo') {
      photo = url
      note = ''
    } else faceOnCreature(url, spotsIn(crop.img, shown, false))
    crop = null
    wobble()
  }
</script>

<input class="file" type="file" accept="image/*" bind:this={fileInput} on:change={onFile} tabindex="-1" aria-hidden="true" />

<div class="builder" role="dialog" aria-modal="true" aria-label="Your avatar" style="--player:{color}">
  <div class="panel kit-panel">
    {#if stream || starting}
      <h2>{target === 'photo' ? 'Smile!' : 'Line up your face'}</h2>
      <div class="finder" class:round={target === 'photo'}>
        <!-- svelte-ignore a11y-media-has-caption -->
        <video bind:this={video} autoplay playsinline muted></video>
        {#if target === 'creature'}<canvas bind:this={overlay}></canvas>{/if}
      </div>
      <p class="hint">
        {#if !stream}Starting camera…{:else if target === 'photo'}Fit your face in the circle.{:else}Googly eyes land on yours. Move around!{/if}
      </p>
      <div class="actions">
        <button type="button" class="secondary" on:click={closeCamera}>Back</button>
        <button type="button" class="primary" disabled={!stream} on:click={capture}>Take photo</button>
      </div>
    {:else if crop && shown}
      <h2>Frame your photo</h2>
      <div
        class="cropper"
        class:round={target === 'photo'}
        class:panning={!!pan}
        bind:this={cropBox}
        bind:clientWidth={cropSize}
        on:pointerdown={onCropDown}
        on:pointermove={onCropMove}
        on:pointerup={onCropUp}
        on:pointercancel={onCropUp}
        on:wheel={onCropWheel}
      >
        <span
          class="picture"
          use:place={crop.img}
          style="width:{crop.w * scale}px;height:{crop.h * scale}px;transform:translate({-shown.sx * scale}px,{-shown.sy * scale}px)"
        ></span>
      </div>
      <label class="size">
        <span>Zoom</span>
        <input
          type="range"
          min="1"
          max={CROP_ZOOM_MAX}
          step="0.01"
          value={crop.zoom}
          style="--fill:{((crop.zoom - 1) / (CROP_ZOOM_MAX - 1)) * 100}%"
          on:input={(e) => setZoom(Number(e.currentTarget.value))}
        />
      </label>
      <p class="hint">Drag to move it around.</p>
      <div class="actions">
        <button type="button" class="secondary" on:click={() => (crop = null)}>Back</button>
        <button type="button" class="primary" on:click={useCrop}>Use photo</button>
      </div>
    {:else}
      <header>
        <h2>Your avatar</h2>
        {#if mode === 'creature'}
          <div class="tools">
            <button type="button" class="secondary icon" disabled={busy || !history.length} on:click={undo} aria-label="Undo">↶</button>
            <button type="button" class="secondary icon" disabled={busy} on:click={surprise} aria-label="Surprise me">🎲</button>
          </div>
        {/if}
      </header>

      <div class="modes" role="tablist" aria-label="Kind of avatar">
        <button type="button" role="tab" aria-selected={mode === 'creature'} class:selected={mode === 'creature'} disabled={busy} on:click={() => setMode('creature')}
          >Creature</button
        >
        <button type="button" role="tab" aria-selected={mode === 'photo'} class:selected={mode === 'photo'} disabled={busy} on:click={() => setMode('photo')}
          >Just a photo</button
        >
      </div>

      {#if mode === 'photo'}
        <div class="stage photo-stage">
          {#if photo}
            <img class="photo" src={photo} alt="Your avatar" width={SELFIE_SIZE} height={SELFIE_SIZE} />
          {:else}
            <span class="placeholder" aria-hidden="true">🙂</span>
          {/if}
        </div>
        <p class="hint" role="status">{note || (photo ? 'This is how everyone will see you.' : 'Take a selfie or pick any picture.')}</p>
        <div class="photo-actions">
          {#if canSelfie}
            <button type="button" class="secondary" disabled={busy} on:click={() => startCamera('photo')}>📸 {photo ? 'Retake' : 'Selfie'}</button>
          {/if}
          <button type="button" class="secondary" disabled={busy} on:click={() => pickPhoto('photo')}>🖼️ {photo ? 'Another' : 'Gallery'}</button>
        </div>
        {#if photo}
          <button type="button" class="link center" disabled={busy} on:click={photoToCreature}>Put this face on a creature instead</button>
        {/if}
      {:else}
        <div
          class="stage"
          class:grabbing={!!grab}
          bind:this={stage}
          bind:clientWidth={stageSize}
          on:pointerdown={onStageDown}
          on:pointermove={onStageMove}
          on:pointerup={onStageUp}
          on:pointercancel={onStageUp}
        >
          <Avatar avatar={encoded} {color} view="full" size={stageSize} />
        </div>
        <p class="hint" role="status">{note || 'Drag the eyes, mouth and nose where you want them.'}</p>

        <div class="tabs" role="tablist" aria-label="Parts">
          {#each TABS as t}
            <button type="button" role="tab" aria-selected={tab === t} class="tab" class:selected={tab === t} on:click={() => (tab = t)}
              >{t.label}</button
            >
          {/each}
        </div>

        <div class="options" role="radiogroup" aria-label={tab.label}>
          {#each options as option (tab.part + option.i)}
            <button
              type="button"
              role="radio"
              aria-checked={creature[tab.part] === option.i}
              aria-label="{tab.label} {option.i + 1}"
              class="option"
              class:selected={creature[tab.part] === option.i}
              disabled={busy}
              on:click={() => choose(option.i)}
            >
              <Avatar avatar={option.avatar} {color} view={tab.view} size={50} still plate={false} />
            </button>
          {/each}
        </div>

        {#if tab.part === 'eyeStyle'}
          <div class="eye-tools">
            <div class="stepper">
              <span>How many</span>
              <button type="button" class="secondary" disabled={busy || creature.eyes.length <= 1} on:click={() => setEyeCount(creature.eyes.length - 1)} aria-label="Fewer eyes">−</button>
              <strong>{creature.eyes.length}</strong>
              <button type="button" class="secondary" disabled={busy || creature.eyes.length >= MAX_EYES} on:click={() => setEyeCount(creature.eyes.length + 1)} aria-label="More eyes">+</button>
            </div>
            {#if creature.eyes.length}
              <label class="size">
                <span>Size</span>
                <input
                  type="range"
                  min={EYE_RADIUS_MIN + 2}
                  max={EYE_RADIUS_MAX - 10}
                  step="1"
                  value={creature.eyes[0].r}
                  style="--fill:{((creature.eyes[0].r - EYE_RADIUS_MIN - 2) / (EYE_RADIUS_MAX - EYE_RADIUS_MIN - 12)) * 100}%"
                  disabled={busy}
                  on:pointerdown={() => (history = [...history.slice(-19), creature])}
                  on:input={(e) => setEyeSize(Number(e.currentTarget.value))}
                />
              </label>
            {/if}
          </div>
        {/if}

        <div class="selfie-row">
          <span class="label">{creature.photo ? 'Your face' : 'Your face on it'}</span>
          {#if canSelfie}
            <button type="button" class="secondary small" disabled={busy} on:click={() => startCamera('creature')} aria-label={creature.photo ? 'Retake selfie' : 'Take a selfie'}
              >📸</button
            >
          {/if}
          <button type="button" class="secondary small" disabled={busy} on:click={() => pickPhoto('creature')} aria-label="Pick a picture">🖼️</button>
          {#if creature.photo}
            <button type="button" class="link" disabled={busy} on:click={removeCreaturePhoto}>Remove</button>
          {/if}
        </div>
      {/if}

      {#if error}<p class="err" role="alert">{error}</p>{/if}
      <div class="actions">
        <button type="button" class="secondary" disabled={busy} on:click={onCancel}>Cancel</button>
        <button type="button" class="primary" disabled={busy || !result} on:click={() => result && onDone(result)}>{busy ? 'Saving…' : doneLabel}</button>
      </div>
    {/if}
  </div>
</div>

<style>
  .builder {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: max(12px, env(safe-area-inset-top)) max(12px, env(safe-area-inset-right)) max(12px, env(safe-area-inset-bottom))
      max(12px, env(safe-area-inset-left));
    background: var(--yui-scrim);
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .panel {
    width: 100%;
    max-width: 440px;
    max-height: 100%;
    margin: auto;
    padding: 14px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    overflow-y: auto;
    color: var(--ink);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  h2 {
    margin: 0;
    font-size: 1.2rem;
  }
  .tools {
    display: flex;
    gap: 8px;
  }
  .icon {
    flex: 0 0 auto;
    width: 48px;
    min-height: 44px;
    padding: 0;
    font-size: 1.35rem;
  }
  .file {
    position: fixed;
    width: 1px;
    height: 1px;
    opacity: 0;
    pointer-events: none;
  }
  /* Creature or photo: one pill split in two, the chosen half pressed in. */
  .modes {
    display: grid;
    grid-template-columns: 1fr 1fr;
    padding: 4px;
    gap: 4px;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-control);
    background: var(--kit-key);
  }
  .modes button {
    min-height: 40px;
    border: 0;
    border-radius: calc(var(--yui-radius-control) - 4px);
    background: none;
    color: var(--muted);
    font: inherit;
    font-weight: 800;
  }
  .modes button.selected {
    background: var(--kit-blue);
    color: #fff;
    box-shadow: inset 0 -3px 0 var(--yui-blue-lip);
  }
  .stage {
    align-self: center;
    width: min(100%, 230px, 34dvh);
    aspect-ratio: 1;
    display: grid;
    place-items: center;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-hero);
    background:
      radial-gradient(circle at 50% 108%, rgba(20, 60, 100, 0.16) 0 34%, transparent 35%),
      linear-gradient(180deg, #fff, var(--kit-key));
    touch-action: none;
    cursor: grab;
  }
  .stage.grabbing {
    cursor: grabbing;
  }
  .photo-stage {
    background: linear-gradient(180deg, #fff, var(--kit-key));
    cursor: default;
  }
  .photo {
    width: 78%;
    height: 78%;
    object-fit: cover;
    border-radius: 50%;
    border: 4px solid #fff;
    box-shadow: 0 0 0 4px var(--player, var(--accent));
  }
  .placeholder {
    width: 78%;
    height: 78%;
    display: grid;
    place-items: center;
    border: 3px dashed var(--kit-key-line);
    border-radius: 50%;
    font-size: 3.5rem;
    opacity: 0.5;
  }
  .photo-actions {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
    gap: 10px;
  }
  .link.center {
    align-self: center;
    color: var(--kit-blue);
  }
  .hint {
    margin: 0;
    text-align: center;
    color: var(--muted);
    font-size: 0.85rem;
    font-weight: 600;
  }
  .tabs {
    display: flex;
    gap: 6px;
    margin: 0 -14px;
    padding: 0 14px 2px;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .tabs::-webkit-scrollbar {
    display: none;
  }
  .tab {
    flex: 0 0 auto;
    min-height: 40px;
    padding: 0 14px;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-control);
    background: var(--kit-key);
    box-shadow: 0 3px 0 var(--kit-key-lip);
    color: var(--ink);
    font: inherit;
    font-weight: 800;
  }
  .tab.selected {
    transform: translateY(3px);
    border-color: var(--yui-blue-lip);
    background: var(--kit-blue);
    box-shadow: none;
    color: #fff;
  }
  .options {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(58px, 1fr));
    gap: 8px;
    padding-bottom: 3px;
  }
  .option {
    aspect-ratio: 1;
    padding: 0;
    display: grid;
    place-items: center;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-hero);
    background: linear-gradient(180deg, #fff 0 50%, var(--kit-key) 50%);
    box-shadow: 0 3px 0 var(--kit-key-lip);
    transition:
      transform var(--yui-press) var(--yui-ease-out),
      box-shadow var(--yui-press) var(--yui-ease-out);
  }
  .option.selected {
    transform: translateY(3px);
    border-color: var(--kit-blue);
    background: #dff1ff;
    box-shadow: 0 0 0 2px var(--kit-blue);
  }
  .eye-tools {
    display: flex;
    flex-direction: column;
    gap: 8px;
    font-weight: 800;
  }
  .stepper {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .stepper span,
  .size span {
    flex: 1;
  }
  .stepper .secondary {
    width: 48px;
    min-height: 40px;
    padding: 0;
    font-size: 1.3rem;
  }
  .stepper strong {
    min-width: 1.5ch;
    text-align: center;
  }
  .size {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .size input {
    flex: 2;
  }
  .selfie-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .selfie-row .label {
    flex: 1;
    font-weight: 800;
  }
  .small {
    width: 52px;
    min-height: 44px;
    padding: 0;
    font-size: 1.3rem;
  }
  .link {
    border: 0;
    background: none;
    color: var(--kit-red);
    font: inherit;
    font-weight: 800;
    text-decoration: underline;
  }
  .actions {
    display: grid;
    grid-template-columns: 1fr 1.4fr;
    gap: 10px;
  }
  .err {
    margin: 0;
    color: var(--kit-red);
    font-weight: 700;
  }
  .finder {
    position: relative;
    align-self: center;
    width: min(100%, 300px, 50dvh);
    aspect-ratio: 1;
    overflow: hidden;
    border: 4px solid #fff;
    border-radius: var(--yui-radius-shell);
    background: #000;
    box-shadow: 0 0 0 4px var(--player, var(--accent));
  }
  .finder video {
    width: 100%;
    height: 100%;
    object-fit: cover;
    transform: scaleX(-1);
  }
  .finder.round,
  .cropper.round {
    border-radius: 50%;
  }
  .cropper {
    position: relative;
    align-self: center;
    width: min(100%, 300px, 50dvh);
    aspect-ratio: 1;
    overflow: hidden;
    border: 4px solid #fff;
    border-radius: var(--yui-radius-hero);
    background: #000;
    box-shadow: 0 0 0 4px var(--player, var(--accent));
    touch-action: none;
    cursor: grab;
  }
  .cropper.panning {
    cursor: grabbing;
  }
  .picture {
    position: absolute;
    top: 0;
    left: 0;
    pointer-events: none;
  }
  .picture :global(img) {
    display: block;
    width: 100%;
    height: 100%;
    max-width: none;
    user-select: none;
    -webkit-user-drag: none;
  }
  .finder canvas {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
  }
</style>
