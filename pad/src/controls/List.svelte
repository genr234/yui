<script lang="ts">
  import { safeImage, usePad } from './context'

  export let props: Record<string, unknown>
  export let style = ''

  const { sink } = usePad()

  type Item = { id: string; title: string; subtitle: string; image: string | null }

  $: id = typeof props.id === 'string' ? props.id : ''
  $: columns = typeof props.columns === 'number' ? Math.max(1, Math.min(3, Math.floor(props.columns))) : 1
  $: items = (Array.isArray(props.items) ? props.items : [])
    .filter((o): o is Record<string, unknown> => !!o && typeof o === 'object' && typeof (o as any).id === 'string')
    .map((o) => ({
      id: o.id as string,
      title: String(o.title ?? ''),
      subtitle: typeof o.subtitle === 'string' ? o.subtitle : '',
      image: safeImage(o.image),
    })) as Item[]

  function tap(item: Item) {
    if (!id) return
    sink.feedback()
    sink.event(id, 'tap', item.id)
  }
</script>

<div class="list" class:grid={columns > 1} style="--cols:{columns};{style}">
  {#each items as item (item.id)}
    <button type="button" class="item" on:click={() => tap(item)}>
      {#if item.image}
        <img src={item.image} alt="" loading="lazy" decoding="async" draggable="false" />
      {:else if columns > 1}
        <span class="ph">{item.title.slice(0, 1)}</span>
      {/if}
      <span class="text">
        <span class="title">{item.title}</span>
        {#if item.subtitle}<span class="sub">{item.subtitle}</span>{/if}
      </span>
    </button>
  {/each}
</div>

<style>
  .list {
    display: flex;
    flex-direction: column;
    gap: 10px;
    overflow-y: auto;
    min-height: 0;
    flex: 1;
    -webkit-overflow-scrolling: touch;
    touch-action: pan-y;
  }
  .list.grid {
    display: grid;
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
    align-content: start;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 14px;
    min-height: 64px;
    padding: 10px 14px;
    border: 0;
    border-radius: var(--yui-radius-hero);
    background: var(--surface-2);
    color: var(--fg);
    text-align: left;
    box-shadow: 0 var(--yui-lip) 0 rgba(0, 0, 0, 0.4), 0 0 0 1px var(--line) inset;
    transition: transform var(--yui-press) var(--yui-ease-out);
  }
  .item:active {
    transform: translateY(var(--yui-lip));
    box-shadow: 0 0 0 3px var(--player) inset;
  }
  .item img {
    flex: 0 0 auto;
    width: 56px;
    height: 56px;
    object-fit: cover;
    border-radius: var(--yui-radius-control);
    background: var(--yui-chrome-fill);
  }
  .text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .title {
    font-weight: 700;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub {
    color: var(--muted);
    font-size: 0.9rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* Cover grid */
  .grid .item {
    flex-direction: column;
    align-items: stretch;
    padding: 8px;
    gap: 8px;
  }
  .grid .item img,
  .grid .ph {
    width: 100%;
    height: auto;
    aspect-ratio: 3 / 4;
  }
  .ph {
    display: grid;
    place-items: center;
    border-radius: var(--yui-radius-control);
    background: var(--yui-chrome-fill);
    color: var(--muted);
    font-size: 2rem;
    font-weight: 800;
  }
  .grid .title,
  .grid .sub {
    text-align: center;
  }
</style>
