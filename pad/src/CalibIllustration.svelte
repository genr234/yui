<script lang="ts">
  /**
   * The picture for each calibration step. Tilt steps show a 3D phone (the
   * camera dot marks its top): a looping demo until the player starts, then
   * the phone's real angle, live. Aim steps show the kiosk (a tall totem with
   * the game's 16:9 band in the middle) and a beam to the same target the
   * kiosk itself is drawing.
   */
  import type { CalibStep, CalibPhase } from './input/calibration'

  export let step: CalibStep
  export let phase: CalibPhase
  /** Tilt steps: degrees turned so far (live), when known. */
  export let angle: number | undefined = undefined

  const FLAT = 64 // rotateX that makes the phone read as lying flat
  $: live = (step === 'pitch' || step === 'roll') && phase !== 'settle' && angle !== undefined
  $: t = Math.max(0, Math.min(90, angle ?? 0)) / 90
  $: phoneStyle = live
    ? step === 'pitch'
      ? `transform: rotateX(${FLAT * (1 - t)}deg)`
      : `transform: rotateX(${FLAT}deg) rotateY(${t * 88}deg)`
    : ''
</script>

<div class="art" aria-hidden="true">
  {#if step === 'rest' || step === 'pitch' || step === 'roll'}
    <div class="stage">
      {#if step === 'rest'}<div class="palm"></div>{/if}
      <div class="phone {step}" class:live style={phoneStyle}>
        <span class="cam"></span>
        <span class="screen"></span>
      </div>
      {#if step === 'rest'}
        <span class="still s1"></span><span class="still s2"></span>
      {/if}
    </div>
  {:else if step === 'aim-center' || step === 'aim-right' || step === 'aim-top'}
    <svg viewBox="0 0 200 170" class="kiosk {step}">
      <!-- the totem -->
      <rect x="62" y="6" width="76" height="132" rx="6" class="body" />
      <rect x="66" y="10" width="68" height="124" rx="3" class="glass" />
      <!-- the game's band: what to aim inside -->
      <rect x="66" y="53" width="68" height="38.25" class="band" />
      {#if step === 'aim-center'}
        <circle cx="100" cy="72" r="9" class="ring" />
        <circle cx="100" cy="72" r="3" class="dot" />
      {:else if step === 'aim-right'}
        <rect x="130" y="53" width="4" height="38.25" rx="2" class="edge" />
      {:else}
        <rect x="66" y="53" width="68" height="4" rx="2" class="edge" />
      {/if}
      <!-- the beam from the phone -->
      <line x1="100" y1="160" x2="100" y2="72" class="beam" />
      <!-- the phone, held like a remote, seen from behind -->
      <rect x="92" y="148" width="16" height="20" rx="3" class="handset" />
    </svg>
  {:else}
    <div class="stage"></div>
  {/if}
</div>

<style>
  .art {
    display: grid;
    place-items: center;
    width: 100%;
    height: 170px;
  }
  .stage {
    position: relative;
    width: 180px;
    height: 170px;
    perspective: 520px;
    display: grid;
    place-items: center;
  }
  .palm {
    position: absolute;
    bottom: 20px;
    width: 120px;
    height: 46px;
    border-radius: 50% 50% 40% 40%;
    background: #f1c7a3;
    box-shadow: inset 0 -6px 0 #d9a67e;
  }
  .phone {
    position: relative;
    width: 64px;
    height: 118px;
    border-radius: 14px;
    background: #1d2633;
    border: 3px solid #0d131c;
    transform-style: preserve-3d;
    transform: rotateX(64deg);
    box-shadow: 0 10px 0 rgba(0, 0, 0, 0.25);
  }
  .phone.live {
    transition: transform 90ms linear;
    animation: none !important;
  }
  .screen {
    position: absolute;
    inset: 12px 6px 10px;
    border-radius: 6px;
    background: linear-gradient(160deg, #6cc4ff, #3d9de0);
  }
  /* The front camera: the "top edge" in every instruction. */
  .cam {
    position: absolute;
    top: 4px;
    left: 50%;
    width: 7px;
    height: 7px;
    margin-left: -3.5px;
    border-radius: 50%;
    background: var(--kit-yellow, #ffc627);
    box-shadow: 0 0 8px var(--kit-yellow, #ffc627);
  }
  .phone.rest {
    animation: rest-breathe 2.4s ease-in-out infinite;
  }
  .phone.pitch {
    transform-origin: 50% 100%;
    animation: tip-up 3.2s ease-in-out infinite;
  }
  .phone.roll {
    animation: roll-side 3.2s ease-in-out infinite;
  }
  .still {
    position: absolute;
    top: 24px;
    width: 30px;
    height: 3px;
    border-radius: 2px;
    background: rgba(255, 255, 255, 0.7);
    animation: still-blink 2.4s ease-in-out infinite;
  }
  .s1 {
    left: 28px;
    transform: rotate(-20deg);
  }
  .s2 {
    right: 28px;
    transform: rotate(20deg);
    animation-delay: 0.3s;
  }
  @keyframes rest-breathe {
    0%,
    100% {
      transform: rotateX(64deg) translateZ(0);
    }
    50% {
      transform: rotateX(64deg) translateZ(4px);
    }
  }
  @keyframes tip-up {
    0%,
    15% {
      transform: rotateX(64deg);
    }
    60%,
    85% {
      transform: rotateX(0deg);
    }
    100% {
      transform: rotateX(64deg);
    }
  }
  @keyframes roll-side {
    0%,
    15% {
      transform: rotateX(64deg) rotateY(0deg);
    }
    60%,
    85% {
      transform: rotateX(64deg) rotateY(88deg);
    }
    100% {
      transform: rotateX(64deg) rotateY(0deg);
    }
  }
  @keyframes still-blink {
    0%,
    100% {
      opacity: 0;
    }
    50% {
      opacity: 1;
    }
  }

  .kiosk {
    width: 220px;
    height: 170px;
    overflow: visible;
  }
  .body {
    fill: #1d2633;
  }
  .glass {
    fill: #2c3a4d;
  }
  .band {
    fill: #3d9de0;
    stroke: #fff;
    stroke-width: 1;
    stroke-dasharray: 3 2;
  }
  .ring {
    fill: none;
    stroke: var(--kit-yellow, #ffc627);
    stroke-width: 2.5;
    transform-origin: 100px 72px;
    animation: ring-pulse 1.4s ease-out infinite;
  }
  .dot {
    fill: #fff;
  }
  .edge {
    fill: var(--kit-yellow, #ffc627);
    animation: edge-glow 0.9s ease-in-out infinite alternate;
  }
  .handset {
    fill: #0d131c;
    stroke: #fff;
    stroke-width: 1.5;
  }
  .beam {
    stroke: var(--kit-yellow, #ffc627);
    stroke-width: 2;
    stroke-dasharray: 4 4;
    animation: beam-flow 0.6s linear infinite;
  }
  /* The beam swings from the middle to the target edge, like the player will. */
  .aim-right .beam {
    animation:
      beam-flow 0.6s linear infinite,
      beam-right 2.8s ease-in-out infinite;
  }
  .aim-top .beam {
    animation:
      beam-flow 0.6s linear infinite,
      beam-top 2.8s ease-in-out infinite;
  }
  @keyframes beam-flow {
    to {
      stroke-dashoffset: -8;
    }
  }
  @keyframes beam-right {
    0%,
    15% {
      transform: rotate(0deg);
    }
    55%,
    90% {
      transform: rotate(18deg);
    }
    100% {
      transform: rotate(0deg);
    }
  }
  @keyframes beam-top {
    0%,
    15% {
      transform: scaleY(1);
    }
    55%,
    90% {
      transform: scaleY(1.17);
    }
    100% {
      transform: scaleY(1);
    }
  }
  .beam {
    transform-origin: 100px 160px;
  }
  @keyframes ring-pulse {
    from {
      transform: scale(0.6);
      opacity: 1;
    }
    to {
      transform: scale(1.6);
      opacity: 0;
    }
  }
  @keyframes edge-glow {
    from {
      opacity: 0.5;
    }
    to {
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .phone,
    .still,
    .ring,
    .edge,
    .beam {
      animation: none !important;
    }
  }
</style>
