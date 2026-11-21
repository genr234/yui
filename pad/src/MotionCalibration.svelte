<script lang="ts">
  /**
   * The guided motion calibration (input/calibration.ts), one step at a
   * time. Shown instead of the layout while a game wants motion and this
   * phone has no full calibration yet, and whenever the player recalibrates.
   */
  import { createEventDispatcher } from 'svelte'
  import type { MotionView } from './input/motion'
  import type { CalibProblem, CalibStep, CalibView } from './input/calibration'
  import CalibIllustration from './CalibIllustration.svelte'

  export let view: MotionView

  const dispatch = createEventDispatcher<{ start: void; skip: void; cancel: void; done: void }>()

  type StepText = { title: string; how: string[]; settle: string; move: string; hold: string }

  const STEPS: Record<CalibStep, StepText> = {
    rest: {
      title: 'Hold it flat and still',
      how: [
        'Lay the phone flat on your open palm, screen up, like a tray.',
        'Tuck your elbow in and keep still until the bar fills (3 s).',
      ],
      settle: 'Hold it flat, screen up…',
      move: 'Hold it flat, screen up…',
      hold: 'Measuring. Keep still…',
    },
    pitch: {
      title: 'Tip the top up',
      how: [
        'From flat, tip the camera end (yellow dot) up until the phone stands straight up, screen facing you.',
        "Hold it there. Don't twist or lean it.",
      ],
      settle: 'Hold it flat, screen up, to start…',
      move: 'Tip the camera end up…',
      hold: 'Hold it upright. Keep still…',
    },
    roll: {
      title: 'Roll it onto its side',
      how: [
        'Flat again, then roll your wrist until the phone stands on a long side, screen facing sideways.',
        'Hold it there. Keep the top and bottom level.',
      ],
      settle: 'Hold it flat, screen up, to start…',
      move: 'Roll one long side down…',
      hold: 'Hold it on its side. Keep still…',
    },
    'aim-center': {
      title: 'Aim at the ring on the kiosk',
      how: [
        'Stand where you will play, facing the kiosk.',
        'Hold the phone like a TV remote, camera end toward the kiosk, and point at the glowing ring in the middle of the game.',
      ],
      settle: 'Point at the ring on the kiosk…',
      move: 'Point at the ring on the kiosk…',
      hold: 'Hold still on the ring…',
    },
    'aim-right': {
      title: 'Aim at the right edge',
      how: ["Don't step sideways. Turn your wrist until you point at the glowing bar on the game's right edge."],
      settle: 'Turn to the glowing right edge…',
      move: 'Turn to the glowing right edge…',
      hold: 'Hold still on the edge…',
    },
    'aim-top': {
      title: 'Aim at the top edge',
      how: ["Now point at the glowing bar on the game's top edge (in the middle of the kiosk, not its top)."],
      settle: 'Point at the glowing top edge…',
      move: 'Point at the glowing top edge…',
      hold: 'Hold still on the edge…',
    },
    verify: {
      title: 'Check the aim',
      how: [
        "Point around the kiosk's game. The dot should follow and reach the box's edge at the game's edge.",
        'If it does, point at the middle and press Done. If not, start over.',
      ],
      settle: '',
      move: '',
      hold: '',
    },
  }

  const PROBLEMS: Record<CalibProblem, (step: CalibStep) => string> = {
    'not-flat': () => 'Hold it level, screen up, like a tray.',
    moving: () => 'Too much movement. Brace your elbow and hold still.',
    further: (s) => (s === 'pitch' ? 'Keep going: stand it all the way up.' : 'Keep going: stand it all the way on its edge.'),
    'wrong-edge': (s) =>
      s === 'pitch'
        ? 'That rolled it sideways. Hold it flat again and tip the top (camera end) up instead.'
        : 'That tipped the top or bottom. Hold it flat again and roll a long side down instead.',
    wobble: (s) =>
      s === 'pitch'
        ? 'It twisted on the way. Hold it flat again and tip it up slowly and straight.'
        : "That didn't match the first tilt. Hold it flat again and roll it slowly, without twisting.",
    'not-remote': () => 'Hold it screen-up, like a TV remote, camera end toward the kiosk.',
    'wrong-way': () => 'That was down. Aim up, at the top edge.',
    'too-big': () => "That's past the game's edge. Point at the glowing bar itself.",
  }

  $: c = view.calib as CalibView | null
  $: text = c ? STEPS[c.step] : null
  $: live = c && text ? (c.phase === 'hold' ? text.hold : c.phase === 'move' ? text.move : text.settle) : ''
  $: problem = c?.problem ? PROBLEMS[c.problem](c.step) : ''
  $: tilt = c?.angle !== undefined && c.target ? Math.min(1, c.angle / c.target) : null
  $: dot = view.preview ?? [0, 0]
</script>

<section class="calib" aria-live="polite">
  {#if !c}
    <h1>Calibrate motion</h1>
    <p class="lead">
      {view.calibrated
        ? 'Recalibrate if aiming drifts, feels too fast or slow, or goes the wrong way.'
        : 'This game uses motion. Before you can play, calibrate this phone once. It takes about a minute.'}
    </p>
    <ul class="needs">
      <li>Stand where you'll play, facing the kiosk</li>
      <li>No table needed</li>
    </ul>
    <button class="primary" on:click={() => dispatch('start')}>Start calibration</button>
    {#if view.calibrated}
      <button class="secondary" on:click={() => dispatch('cancel')}>Keep the current one</button>
    {:else}
      <button class="secondary" on:click={() => dispatch('skip')}>Play without motion</button>
    {/if}
  {:else if text}
    <div class="top">
      <span class="count">Step {c.index + 1} of {c.total}</span>
      <button class="link" on:click={() => dispatch('cancel')}>Cancel</button>
    </div>
    <ol class="dots" aria-hidden="true">
      {#each Array(c.total) as _, i}
        <li class:done={i < c.index} class:now={i === c.index}></li>
      {/each}
    </ol>

    <h1>{c.noGyro ? 'No gyroscope' : text.title}</h1>

    {#if c.step !== 'verify'}
      <CalibIllustration step={c.step} phase={c.phase} angle={c.angle} />
    {/if}

    {#if c.step === 'verify'}
      {#if c.noGyro}
        <p class="lead">This phone has no gyroscope, so you aim by touch. Tilting and shaking are calibrated and work.</p>
      {:else}
        <ol class="how">
          {#each text.how as line}<li>{line}</li>{/each}
        </ol>
        <div class="box" role="img" aria-label="Aim preview">
          <span class="cross"></span>
          <span class="dot" style="left: {50 + dot[0] * 50}%; top: {50 + dot[1] * 50}%"></span>
        </div>
      {/if}
      <button class="primary" on:click={() => dispatch('done')}>Done</button>
      <button class="secondary" on:click={() => dispatch('start')}>Start over</button>
    {:else}
      <ol class="how">
        {#each text.how as line}<li>{line}</li>{/each}
      </ol>
      <div class="status" class:bad={!!problem}>
        <p>{problem || live}</p>
        {#if tilt !== null && c.phase !== 'hold'}
          <div class="meter" style="--p: {tilt}"><span></span></div>
          <small>{c.angle}° of {c.target}°</small>
        {:else}
          <div class="meter hold" style="--p: {c.phase === 'hold' ? c.progress : 0}"><span></span></div>
        {/if}
      </div>
    {/if}
  {/if}
</section>

<style>
  .calib {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 14px;
    padding: 20px 20px 28px;
    max-width: 480px;
    width: 100%;
    margin: 0 auto;
    text-align: center;
  }
  h1 {
    margin: 4px 0 0;
  }
  .lead {
    margin: 0;
    color: var(--muted);
  }
  .needs {
    margin: 0;
    padding: 12px 16px 12px 32px;
    border-radius: var(--yui-radius-card);
    background: var(--yui-chrome-fill);
    text-align: left;
  }
  .top {
    display: flex;
    justify-content: space-between;
    align-items: center;
    width: 100%;
  }
  .count {
    font-weight: 800;
    color: var(--muted);
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
  .dots {
    display: flex;
    gap: 8px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .dots li {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--yui-chrome-fill);
  }
  .dots li.done {
    background: var(--kit-green);
  }
  .dots li.now {
    background: var(--fg);
  }
  .how {
    margin: 0;
    padding: 14px 16px 14px 34px;
    border-radius: var(--yui-radius-card);
    background: var(--surface-2);
    color: var(--ink);
    text-align: left;
    line-height: 1.4;
  }
  .how li + li {
    margin-top: 6px;
  }
  .status {
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
  }
  .status p {
    margin: 0;
    min-height: 2.6em;
    font-weight: 800;
  }
  .status.bad p {
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
    background: var(--fg);
    transition: width 80ms linear;
  }
  .meter.hold span {
    background: var(--kit-green);
  }
  .box {
    position: relative;
    width: 100%;
    max-width: 320px;
    aspect-ratio: 16 / 9;
    border: 3px solid var(--fg);
    border-radius: var(--yui-radius-control);
    background: rgba(0, 0, 0, 0.18);
  }
  .cross {
    position: absolute;
    left: 50%;
    top: 50%;
    width: 14px;
    height: 14px;
    margin: -7px 0 0 -7px;
    border: 2px solid var(--muted);
    border-radius: 50%;
  }
  .dot {
    position: absolute;
    width: 18px;
    height: 18px;
    margin: -9px 0 0 -9px;
    border-radius: 50%;
    background: var(--kit-yellow);
    box-shadow: 0 0 0 3px rgba(0, 0, 0, 0.25);
  }
  .primary,
  .secondary {
    width: 100%;
    max-width: 320px;
  }
</style>
