<script lang="ts">
  import type { SeatMate } from '@yui/protocol'
  import Avatar from './Avatar.svelte'
  import { colorHex } from './colors'

  export let from: SeatMate
  export let slot: number
  export let secondsLeft: number
  export let onAnswer: (accept: boolean) => void
</script>

<div class="scrim" role="presentation">
  <div class="card kit-panel" role="alertdialog" aria-modal="true" aria-label="Seat offer" style="--player:{colorHex(from.color)}">
    <h2 class="band">Take a seat?</h2>
    <div class="who">
      <Avatar avatar={from.avatar ?? null} size={56} />
      <p><strong>{from.name}</strong> offers you seat <strong>P{slot}</strong></p>
    </div>
    <p class="muted">Offer ends in {secondsLeft}s</p>
    <div class="actions">
      <button class="secondary" type="button" on:click={() => onAnswer(false)}>No thanks</button>
      <button class="primary" type="button" on:click={() => onAnswer(true)}>Accept</button>
    </div>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 35;
    display: flex;
    align-items: flex-end;
    justify-content: center;
    background: var(--yui-scrim);
  }
  .card {
    width: 100%;
    max-width: 480px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 0 max(18px, env(safe-area-inset-right)) max(24px, env(safe-area-inset-bottom)) max(18px, env(safe-area-inset-left));
    border-bottom: 0;
    border-radius: var(--yui-radius-shell) var(--yui-radius-shell) 0 0;
    overflow: hidden;
    animation: up 220ms var(--yui-ease-out);
  }
  @keyframes up {
    from {
      transform: translateY(40px);
      opacity: 0;
    }
  }
  h2,
  p {
    margin: 0;
  }
  .band {
    margin: 0 calc(-1 * max(18px, env(safe-area-inset-left))) 4px;
    padding: 14px 18px;
    border-bottom: 2px solid #cf9700;
    background: linear-gradient(180deg, #ffd65a 0 50%, var(--kit-yellow) 50%);
    color: #6b4b00;
    font-size: 1.25rem;
    font-weight: 900;
    text-align: center;
  }
  .who {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .who p {
    font-size: 1.1rem;
    font-weight: 700;
  }
  .actions {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
    margin-top: 6px;
  }
  .actions button {
    min-height: 56px;
  }
</style>
