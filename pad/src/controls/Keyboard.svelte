<script lang="ts">
  import { LiveKeyboard } from './drivers'
  import { usePad } from './context'

  export let props: Record<string, unknown>
  export let style = ''

  const { sink } = usePad()

  $: id = typeof props.id === 'string' ? props.id : ''
  $: mode = props.mode === 'field' ? 'field' : 'live'
  $: placeholder = typeof props.placeholder === 'string' ? props.placeholder : mode === 'live' ? 'Tap to type' : ''
  $: submitLabel = typeof props.submit_label === 'string' && props.submit_label ? props.submit_label : 'Send'

  // live
  const live = new LiveKeyboard(sink)
  let liveEl: HTMLInputElement
  let liveValue = live.value
  let typing = false

  function onLiveInput() {
    liveValue = live.input(liveEl.value)
    if (liveEl.value !== liveValue) liveEl.value = liveValue
  }
  function onLiveKey(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault()
      liveValue = live.enter()
      liveEl.value = liveValue
    }
  }
  function placeCaret() {
    // Keep the caret after the sentinel so Backspace on "empty" still deletes something.
    const n = liveEl.value.length
    liveEl.setSelectionRange(n, n)
  }

  // field
  let fieldValue = ''
  let lastProp: unknown
  $: if (props.value !== lastProp) {
    lastProp = props.value
    fieldValue = typeof props.value === 'string' ? props.value : ''
  }
  function submit() {
    if (!id) return
    sink.event(id, 'submit', fieldValue)
    sink.feedback()
  }
</script>

{#if mode === 'live'}
  <label class="live" class:typing {style}>
    <span class="hint">{typing ? 'Typing on the kiosk…' : placeholder}</span>
    <input
      bind:this={liveEl}
      value={liveValue}
      autocomplete="off"
      autocorrect="off"
      autocapitalize="off"
      spellcheck="false"
      enterkeyhint="send"
      aria-label={placeholder || 'Keyboard'}
      on:input={onLiveInput}
      on:keydown={onLiveKey}
      on:focus={() => {
        typing = true
        placeCaret()
      }}
      on:blur={() => (typing = false)}
      on:click={placeCaret}
    />
  </label>
{:else}
  <form class="field" {style} on:submit|preventDefault={submit}>
    <input bind:value={fieldValue} {placeholder} enterkeyhint="send" aria-label={placeholder || 'Text'} />
    <button class="primary" type="submit">{submitLabel}</button>
  </form>
{/if}

<style>
  .live {
    position: relative;
    display: grid;
    place-items: center;
    min-height: 64px;
    border-radius: var(--yui-radius-hero);
    background: var(--yui-chrome-fill);
    box-shadow: 0 0 0 1px var(--line) inset;
    overflow: hidden;
  }
  .live.typing {
    box-shadow: 0 0 0 2px var(--player) inset;
  }
  .hint {
    color: var(--muted);
    font-weight: 600;
    pointer-events: none;
  }
  /* The real input covers the box but is invisible: typed text goes to the kiosk, not here. */
  .live input {
    position: absolute;
    inset: 0;
    opacity: 0;
    border: 0;
    caret-color: transparent;
    font-size: 16px;
  }
  .field {
    display: flex;
    gap: 10px;
    align-items: stretch;
  }
  .field input {
    flex: 1;
    min-width: 0;
  }
  .field button {
    flex: 0 0 auto;
  }
</style>
