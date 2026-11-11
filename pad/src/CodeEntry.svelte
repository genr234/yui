<script lang="ts">
  import { LINK_CODE_LENGTH, isCompleteCode, normaliseCode } from './account'

  /** Called with the six digits. Reject to show `error` and let the player retry. */
  export let onSubmit: (code: string) => void | Promise<void>
  export let onCancel: () => void
  export let submitLabel = 'Use this code'
  export let busy = false
  export let error = ''

  let value = ''
  $: ready = isCompleteCode(value) && !busy

  function input(e: Event & { currentTarget: HTMLInputElement }) {
    value = normaliseCode(e.currentTarget.value)
    e.currentTarget.value = value
  }
</script>

<div class="entry">
  <label class="field">
    <span>Phone code</span>
    <input
      class="code"
      inputmode="numeric"
      pattern="[0-9]*"
      autocomplete="one-time-code"
      maxlength={LINK_CODE_LENGTH + 4}
      placeholder="123456"
      aria-label="6-digit phone code"
      {value}
      on:input={input}
      on:keydown={(e) => e.key === 'Enter' && ready && (e.preventDefault(), onSubmit(value))}
    />
  </label>
  <p class="muted hint">Ask for a phone code on your account page or in Yui's Settings.</p>
  {#if error}<p class="err" role="alert">{error}</p>{/if}
  <div class="actions">
    <button class="secondary" type="button" disabled={busy} on:click={onCancel}>Cancel</button>
    <button class="primary" type="button" disabled={!ready} on:click={() => onSubmit(value)}>{busy ? 'Checking…' : submitLabel}</button>
  </div>
</div>

<style>
  .entry {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px;
    border: 2px solid var(--kit-panel-line);
    border-radius: var(--yui-radius-hero);
    background: var(--kit-panel);
    color: var(--ink);
    --muted: var(--ink-muted);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 8px;
    font-weight: 700;
  }
  .code {
    min-height: 56px;
    text-align: center;
    font-size: 1.8rem;
    font-weight: 900;
    letter-spacing: 0.3em;
    font-variant-numeric: tabular-nums;
  }
  .hint {
    margin: 0;
    font-size: 0.9rem;
  }
  .err {
    margin: 0;
    color: var(--kit-red);
    font-weight: 800;
  }
  .actions {
    display: grid;
    grid-template-columns: 1fr 1.4fr;
    gap: 10px;
  }
  .actions button {
    min-height: 52px;
  }
</style>
