<script lang="ts">
  import { usePad } from './context'

  export let props: Record<string, unknown>
  export let style = ''

  const { sink } = usePad()

  type Option = { id: string; label: string; color?: string }

  $: id = typeof props.id === 'string' ? props.id : ''
  $: options = (Array.isArray(props.options) ? props.options : [])
    .filter((o): o is Record<string, unknown> => !!o && typeof o === 'object' && typeof (o as any).id === 'string')
    .map((o) => ({
      id: o.id as string,
      label: String(o.label ?? o.id),
      color: typeof o.color === 'string' && /^#[0-9a-f]{3,8}$/i.test(o.color) ? o.color : undefined,
    })) as Option[]

  // Optimistic: highlight the tap until the kiosk sends a new `selected`.
  let selected: string | null = null
  let lastProp: unknown
  $: if (props.selected !== lastProp) {
    lastProp = props.selected
    selected = typeof props.selected === 'string' ? props.selected : null
  }

  function pick(o: Option) {
    if (!id) return
    selected = o.id
    sink.feedback()
    sink.event(id, 'tap', o.id)
  }
</script>

<div class="choice" {style} role="radiogroup">
  {#each options as o (o.id)}
    <button
      type="button"
      role="radio"
      aria-checked={selected === o.id}
      class:selected={selected === o.id}
      style={o.color ? `--opt:${o.color}` : ''}
      on:click={() => pick(o)}
    >
      {#if o.color}<span class="swatch"></span>{/if}
      <span class="label">{o.label}</span>
    </button>
  {/each}
</div>

<style>
  .choice {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: 10px;
  }
  button {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 60px;
    padding: 12px 16px;
    border: 0;
    border-radius: var(--yui-radius-hero);
    background: var(--surface-2);
    color: var(--fg);
    font-weight: 700;
    font-size: 1.05rem;
    text-align: left;
    box-shadow: 0 var(--yui-lip) 0 rgba(0, 0, 0, 0.4), 0 0 0 1px var(--line) inset;
    transition: transform var(--yui-press) var(--yui-ease-out);
  }
  button:active {
    transform: translateY(var(--yui-lip));
  }
  button.selected {
    box-shadow: 0 var(--yui-lip) 0 rgba(0, 0, 0, 0.4), 0 0 0 3px var(--opt, var(--player)) inset;
  }
  .swatch {
    flex: 0 0 auto;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background: var(--opt);
  }
  .label {
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
