<script lang="ts">
  import Icon from './Icon.svelte'
  import type { ConnState } from './net/session'

  /** Drives the strip from the session's state (see net/session.ts). */
  export let conn: ConnState

  type Step = 'todo' | 'active' | 'done' | 'failed'
  // Kiosk = the ActionCable signalling link. Direct link = the WebRTC data
  // channel the buttons travel on. `fallback` means the cable works but the
  // direct link did not come up.
  $: kiosk = (conn === 'online' || conn === 'fallback' ? 'done' : 'active') as Step
  $: direct = (conn === 'online' ? 'done' : conn === 'fallback' ? 'failed' : 'todo') as Step
  $: steps = [
    { name: 'Kiosk', state: kiosk },
    { name: 'Direct link', state: direct },
  ]
</script>

<ol class="steps kit-panel" aria-label="Connection progress">
  {#each steps as s, i (s.name)}
    {#if i > 0}<li class="join" class:lit={steps[i - 1].state === 'done'} aria-hidden="true"></li>{/if}
    <li class="step {s.state}">
      <span class="bubble">
        {#if s.state === 'done'}<Icon name="checkmark" size={20} />
        {:else if s.state === 'failed'}<Icon name="cross" size={18} />
        {:else if s.state === 'active'}<span class="spin"></span>
        {/if}
      </span>
      <span class="label">{s.name}</span>
      <span class="sr">{s.state === 'done' ? 'connected' : s.state === 'failed' ? 'failed' : s.state === 'active' ? 'connecting' : 'waiting'}</span>
    </li>
  {/each}
</ol>

<style>
  .steps {
    display: flex;
    align-items: flex-start;
    justify-content: center;
    gap: 0;
    margin: 0;
    padding: 14px 18px 12px;
    list-style: none;
  }
  .step {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    min-width: 84px;
    font-size: 0.85rem;
    font-weight: 800;
    color: var(--ink-muted);
  }
  .bubble {
    --c: var(--kit-panel-lip);
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    border: 2px solid color-mix(in srgb, var(--c), #000 26%);
    border-radius: 50%;
    background: linear-gradient(180deg, color-mix(in srgb, var(--c), #fff 16%) 0 50%, var(--c) 50%);
    color: #fff;
    box-shadow: 0 3px 0 color-mix(in srgb, var(--c), #000 26%);
  }
  .active .bubble {
    --c: var(--kit-yellow);
  }
  .done .bubble {
    --c: var(--kit-green);
  }
  .failed .bubble {
    --c: var(--kit-red);
  }
  .done,
  .failed,
  .active {
    color: var(--ink);
  }
  /* The rail between the two dots; it fills green once the first step is done. */
  .join {
    flex: 0 0 40px;
    height: 8px;
    margin-top: 16px;
    border-radius: 99px;
    background: var(--kit-well);
    box-shadow: inset 0 2px 0 rgba(0, 0, 0, 0.12);
  }
  .join.lit {
    background: var(--kit-green);
  }
  .spin {
    width: 18px;
    height: 18px;
    border: 3px solid rgba(255, 255, 255, 0.5);
    border-top-color: #fff;
    border-radius: 50%;
    animation: turn 0.9s linear infinite;
  }
  @keyframes turn {
    to {
      transform: rotate(360deg);
    }
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .step {
    position: relative;
  }
  @media (prefers-reduced-motion: reduce) {
    .spin {
      animation-duration: 3s;
    }
  }
</style>
