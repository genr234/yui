<script lang="ts">
  /**
   * The guided motion calibration (input/calibration.ts), one step at a
   * time. Shown instead of the layout while a game wants motion and this
   * phone has no full calibration yet, and whenever the player recalibrates.
   */
  import { createEventDispatcher } from 'svelte'
  import type { MotionView } from './input/motion'
  import type { CalibProblem, CalibStep, CalibView } from './input/calibration'

  export let view: MotionView

  const dispatch = createEventDispatcher<{ start: void; skip: void; cancel: void; done: void }>()

  type StepText = { title: string; how: string[]; settle: string; move: string; hold: string }

  const STEPS: Record<CalibStep, StepText> = {
    rest: {
      title: 'Lay it on a table',
      how: [
        'Put the phone on a flat table, screen facing up. Not your lap or a sofa.',
        'Take your hand off it completely.',
        "Don't touch or bump the table until the bar fills (2 seconds).",
      ],
      settle: 'Waiting for the phone to lie flat and still…',
      move: 'Waiting for the phone to lie flat and still…',
      hold: 'Measuring. Keep everything still…',
    },
    pitch: {
      title: 'Stand it up',
      how: [
        'Start with the phone flat on the table, screen up.',
        'Keep the bottom edge on the table and lift the top edge (the end with the front camera).',
        'Keep lifting until the phone stands straight up, then hold it there without moving.',
        "Don't twist it or let it tip sideways on the way up.",
      ],
      settle: 'Lay it flat, screen up, to start…',
      move: 'Lift the top edge…',
      hold: 'Hold it upright. Keep still…',
    },
    roll: {
      title: 'Tip it onto its side',
      how: [
        'Lay the phone flat on the table again, screen up.',
        'Keep one long side edge on the table and lift the other long edge.',
        'Keep lifting until it stands on that edge, then hold it there without moving.',
        "Don't twist it, and keep the top and bottom level.",
      ],
      settle: 'Lay it flat, screen up, to start…',
      move: 'Lift one long side…',
      hold: 'Hold it on its edge. Keep still…',
    },
    'aim-center': {
      title: 'Aim at the middle of the TV',
      how: [
        'Pick up the phone and stand or sit exactly where you will play.',
        'Hold it like a TV remote: screen facing up, front-camera end pointing at the TV.',
        'Aim at the exact middle of the TV screen and hold still.',
      ],
      settle: 'Aim at the middle of the TV and hold still…',
      move: 'Aim at the middle of the TV and hold still…',
      hold: 'Hold still on the middle…',
    },
    'aim-right': {
      title: 'Aim at the right edge',
      how: [
        "Stay where you are. Don't step sideways.",
        "Turn the phone with your wrist until it aims at the middle of the TV's right edge.",
        'Hold still on the edge.',
      ],
      settle: "Turn to the TV's right edge…",
      move: "Turn to the TV's right edge…",
      hold: 'Hold still on the right edge…',
    },
    'aim-top': {
      title: 'Aim at the top edge',
      how: ["Still in the same spot, aim at the middle of the TV's top edge.", 'Hold still on the edge.'],
      settle: "Aim at the middle of the TV's top edge…",
      move: "Aim at the middle of the TV's top edge…",
      hold: 'Hold still on the top edge…',
    },
    verify: {
      title: 'Check the aim',
      how: [
        'Point around the TV. The dot should follow your aim.',
        "The dot should reach the box's edge when you aim at the TV's edge.",
        'If it does, aim at the middle of the TV and press Done. If not, start over.',
      ],
      settle: '',
      move: '',
      hold: '',
    },
  }

  const PROBLEMS: Record<CalibProblem, (step: CalibStep) => string> = {
    'not-flat': () => "It isn't lying flat. Use a level table, screen up.",
    moving: () => "Something moved it. Let go and don't touch the table.",
    further: (s) => (s === 'pitch' ? 'Keep going: stand it all the way up.' : 'Keep going: stand it all the way on its edge.'),
    'wrong-edge': (s) =>
      s === 'pitch'
        ? 'That tipped a side edge. Lay it flat and lift the top (camera end) instead.'
        : 'That lifted the top or bottom. Lay it flat and lift a long side instead.',
    wobble: (s) =>
      s === 'pitch'
        ? 'It twisted on the way up. Lay it flat and lift it again, slowly and straight.'
        : "That didn't match the first tilt. Lay it flat and tip it again, slowly, without twisting.",
    'not-remote': () => 'Hold it screen-up, like a TV remote, not standing up.',
    'wrong-way': () => 'That was down. Aim up, at the top edge.',
    'too-big': () => "That's past the TV's edge. Aim back at the edge itself.",
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
      <li>A flat table within reach</li>
      <li>The spot you'll play from, facing the TV</li>
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
