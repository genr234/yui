<script lang="ts">
  import { askText, type ProfileAsk } from './account'

  export let ask: ProfileAsk
  export let account: string | null = null
  export let busy = false
  export let onAnswer: (allow: boolean) => void
</script>

<div class="scrim" role="presentation">
  <div class="sheet" role="alertdialog" aria-modal="true" aria-label="Save your progress">
    <h2>Save your progress?</h2>
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
    backdrop-filter: blur(6px);
    -webkit-backdrop-filter: blur(6px);
  }
  .sheet {
    width: 100%;
    max-width: 480px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 20px max(20px, env(safe-area-inset-right)) max(24px, env(safe-area-inset-bottom))
      max(20px, env(safe-area-inset-left));
    border-radius: var(--yui-radius-shell) var(--yui-radius-shell) 0 0;
    background-color: #142233;
    box-shadow: 0 -20px 60px rgba(0, 0, 0, 0.45), 0 0 0 1px var(--line) inset;
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
