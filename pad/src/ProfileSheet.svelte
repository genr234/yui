<script lang="ts">
  import { askText, type ProfileAsk } from './account'

  export let ask: ProfileAsk
  export let account: string | null = null
  export let busy = false
  export let onAnswer: (allow: boolean) => void
</script>

<div class="scrim" role="presentation">
  <div class="sheet kit-panel" role="alertdialog" aria-modal="true" aria-label="Save your progress">
    <h2 class="band">Save your progress?</h2>
    <p>{askText(ask, account)}</p>
    <p class="muted">It can then keep your scores and unlocks for you, on any Yui kiosk. You can change your mind from your account page.</p>
    <div class="actions">
      <button class="secondary" type="button" disabled={busy} on:click={() => onAnswer(false)}>Not now</button>
      <button class="primary" type="button" disabled={busy} on:click={() => onAnswer(true)}>{busy ? 'Saving…' : 'Allow'}</button>
    </div>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 30;
    display: flex;
    align-items: flex-end;
    justify-content: center;
    background: var(--yui-scrim);
  }
  .sheet {
    width: 100%;
    max-width: 480px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 0 max(18px, env(safe-area-inset-right)) max(24px, env(safe-area-inset-bottom))
      max(18px, env(safe-area-inset-left));
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
  /* The yellow title strip, like a reward panel. */
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
  p:not(.muted) {
    font-size: 1.1rem;
    font-weight: 700;
  }
  .actions {
    display: grid;
    grid-template-columns: 1fr 1.4fr;
    gap: 10px;
    margin-top: 6px;
  }
  .actions button {
    min-height: 56px;
  }
</style>
