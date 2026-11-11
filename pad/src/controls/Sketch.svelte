<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { capture, usePad } from './context'
  import Icon from '../Icon.svelte'
  import { SketchEngine, catmullRom, fitAspect, normalize, scaleSize, sketchState, type Stroke } from './sketch'

  export let props: Record<string, unknown>
  export let style = ''

  const { sink } = usePad()

  const SAFE_COLOR = /^(#[0-9a-f]{3,8}|[a-z]{3,24}|(rgb|hsl)a?\([\d\s.,%/-]{1,40}\))$/i
  const safe = (c: unknown): c is string => typeof c === 'string' && SAFE_COLOR.test(c.trim())

  $: id = typeof props.id === 'string' ? props.id : ''
  $: colors = (Array.isArray(props.colors) ? props.colors : []).filter(safe).slice(0, 12) as string[]
  $: sizes = (Array.isArray(props.sizes) ? props.sizes : [])
    .filter((n): n is number => typeof n === 'number' && Number.isFinite(n) && n > 0)
    .map((n) => Math.min(n, 64))
    .slice(0, 4)
  $: background = safe(props.background) ? props.background : '#ffffff'
  $: disabled = props.disabled === true

  let color = ''
  let size = 0
  $: if (!colors.includes(color)) color = colors[0] ?? '#000000'
  $: if (!sizes.includes(size)) size = sizes[0] ?? 4

  let engine: SketchEngine | null = null
  let armed = false
  let canUndo = false
  $: rebuild(id)

  function rebuild(id: string) {
    engine?.dispose()
    sketchState.bind(id)
    engine = new SketchEngine(sink, id, sketchState)
    engine.onChange = () => {
      armed = engine!.clearArmed
      canUndo = sketchState.strokes.length > 0
      dirty = true
      schedule()
    }
    armed = false
    canUndo = sketchState.strokes.length > 0
    dirty = true
    schedule()
  }

  // ---- canvas ----
  let stage: HTMLDivElement
  let canvas: HTMLCanvasElement
  let boxW = 0
  let boxH = 0
  let dpr = 1
  let dirty = true
  let raf = 0
  let ro: ResizeObserver | undefined

  function measure() {
    if (!stage) return
    const box = fitAspect(stage.clientWidth, stage.clientHeight)
    boxW = Math.floor(box.width)
    boxH = Math.floor(box.height)
    dpr = Math.min(3, (typeof devicePixelRatio === 'number' && devicePixelRatio) || 1)
    dirty = true
    schedule()
  }

  function schedule() {
    if (raf || typeof requestAnimationFrame === 'undefined') return
    raf = requestAnimationFrame(() => {
      raf = 0
      if (dirty) draw()
    })
  }

  function paint(c: CanvasRenderingContext2D, s: Stroke) {
    const w = s.size ? scaleSize(s.size, boxW) : 1
    c.strokeStyle = s.color
    c.fillStyle = s.color
    c.lineWidth = w
    c.lineCap = 'round'
    c.lineJoin = 'round'
    const pts = s.points.map((p) => [p[0] * boxW, p[1] * boxH] as [number, number])
    if (!pts.length) return
    if (pts.length === 1) {
      c.beginPath()
      c.arc(pts[0][0], pts[0][1], w / 2, 0, Math.PI * 2)
      c.fill()
      return
    }
    c.beginPath()
    c.moveTo(pts[0][0], pts[0][1])
    for (const seg of catmullRom(pts)) c.bezierCurveTo(seg.c1[0], seg.c1[1], seg.c2[0], seg.c2[1], seg.to[0], seg.to[1])
    c.stroke()
  }

  function draw() {
    dirty = false
    const c = canvas?.getContext('2d')
    if (!c || !boxW) return
    c.setTransform(dpr, 0, 0, dpr, 0, 0)
    c.clearRect(0, 0, boxW, boxH)
    for (const s of sketchState.strokes) paint(c, s)
  }

  onMount(() => {
    measure()
    if (typeof ResizeObserver !== 'undefined') {
      ro = new ResizeObserver(measure)
      ro.observe(stage)
    }
  })
  onDestroy(() => {
    ro?.disconnect()
    if (raf && typeof cancelAnimationFrame !== 'undefined') cancelAnimationFrame(raf)
    engine?.dispose()
  })

  // ---- pointer ----
  let activePointer: number | null = null

  function pos(e: PointerEvent): [number, number] {
    return normalize(e.clientX, e.clientY, canvas.getBoundingClientRect())
  }

  function down(e: PointerEvent) {
    e.preventDefault()
    if (disabled || !engine || activePointer !== null || !e.isPrimary) return
    activePointer = e.pointerId
    capture(e)
    const [x, y] = pos(e)
    engine.begin(color, size, x, y)
  }
  function move(e: PointerEvent) {
    if (e.pointerId !== activePointer || !engine) return
    e.preventDefault()
    const list = typeof e.getCoalescedEvents === 'function' ? e.getCoalescedEvents() : []
    for (const ev of list.length ? list : [e]) engine.add(...pos(ev))
  }
  function up(e: PointerEvent) {
    if (e.pointerId !== activePointer) return
    activePointer = null
    engine?.end()
  }

  function undo() {
    if (!disabled) {
      sink.feedback()
      engine?.undo()
    }
  }
  function clear() {
    if (disabled) return
    sink.feedback()
    engine?.pressClear()
  }
</script>

<div class="sketch" class:disabled {style}>
  <div class="stage" bind:this={stage}>
    <canvas
      bind:this={canvas}
      width={Math.max(1, Math.round(boxW * dpr))}
      height={Math.max(1, Math.round(boxH * dpr))}
      style="width:{boxW}px;height:{boxH}px;background:{background}"
      aria-label="Drawing area"
      on:pointerdown={down}
      on:pointermove={move}
      on:pointerup={up}
      on:pointercancel={up}
      on:lostpointercapture={up}
      on:touchstart|preventDefault
      on:contextmenu|preventDefault
    ></canvas>
  </div>
  <div class="bar">
    <div class="palette" role="radiogroup" aria-label="Color">
      {#each colors as c (c)}
        <button
          type="button"
          role="radio"
          aria-checked={c === color}
          aria-label={c}
          class="chip"
          class:selected={c === color}
          style="--c:{c}"
          {disabled}
          on:click={() => ((color = c), sink.feedback())}
        ></button>
      {/each}
    </div>
    <div class="row">
      <div class="sizes" role="radiogroup" aria-label="Brush size">
        {#each sizes as s (s)}
          <button
            type="button"
            role="radio"
            aria-checked={s === size}
            aria-label="Brush {s}"
            class="size"
            class:selected={s === size}
            {disabled}
            on:click={() => ((size = s), sink.feedback())}
          >
            <span style="width:{Math.min(28, 4 + s * 1.6)}px;height:{Math.min(28, 4 + s * 1.6)}px"></span>
          </button>
        {/each}
      </div>
      <button type="button" class="act" disabled={disabled || !canUndo} on:click={undo}><Icon name="arrow_rotate" size={18} />Undo</button>
      <button type="button" class="act" class:armed disabled={disabled} on:click={clear}><Icon name="cross" size={16} />{armed ? 'Tap again' : 'Clear'}</button>
    </div>
  </div>
</div>

<style>
  .sketch {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    overscroll-behavior: none;
  }
  .sketch.disabled {
    filter: grayscale(1);
    opacity: 0.55;
  }
  .stage {
    flex: 1;
    min-height: 120px;
    display: grid;
    place-items: center;
    overflow: hidden;
  }
  canvas {
    display: block;
    border: 3px solid var(--kit-panel-line);
    border-radius: var(--yui-radius-card);
    box-shadow: 0 var(--yui-lip) 0 var(--kit-panel-lip);
    touch-action: none;
    -webkit-touch-callout: none;
  }
  .bar {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .palette,
  .row,
  .sizes {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .row {
    justify-content: space-between;
  }
  button {
    border: 0;
    color: var(--ink);
    font: inherit;
    font-weight: 700;
    touch-action: manipulation;
  }
  button:disabled {
    pointer-events: none;
  }
  .chip {
    width: 44px;
    height: 44px;
    border-radius: 50%;
    border: 3px solid #fff;
    background: var(--c);
    box-shadow: 0 3px 0 rgba(14, 64, 112, 0.35);
  }
  .chip.selected {
    transform: translateY(3px);
    box-shadow: 0 0 0 3px var(--ink);
  }
  .size {
    width: 44px;
    height: 44px;
    border: 2px solid var(--kit-key-line);
    border-radius: 50%;
    background: var(--kit-key);
    display: grid;
    place-items: center;
    box-shadow: 0 3px 0 var(--kit-key-lip);
  }
  .size span {
    display: block;
    border-radius: 50%;
    background: var(--ink);
  }
  .size.selected {
    transform: translateY(3px);
    border-color: var(--player);
    box-shadow: inset 0 0 0 2px var(--player);
  }
  .act {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font-weight: 800;
    min-width: 88px;
    min-height: 48px;
    padding: 0 16px;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-hero);
    background: linear-gradient(180deg, #fff 0 50%, var(--kit-key) 50%);
    box-shadow: 0 var(--yui-lip) 0 var(--kit-key-lip);
  }
  .act:active {
    transform: translateY(var(--yui-lip));
    box-shadow: none;
  }
  .act:disabled {
    opacity: 0.5;
  }
  .act.armed {
    border-color: #b8263a;
    background: var(--kit-red);
    color: #fff;
    box-shadow: 0 var(--yui-lip) 0 #b8263a;
  }
</style>
