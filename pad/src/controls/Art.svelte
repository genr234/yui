<script lang="ts">
  import { artSize, svg, type Box } from './art'

  /** Art file (without .svg), e.g. "button_circle". Its glow outline is "hl_<name>". */
  export let name: string
  /**
   * whole: scaled to fit.
   * cap: left and right ends keep their shape, the middle stretches (pill buttons, any width).
   * nine: four fixed corners, stretching edges (block buttons and panels, any size).
   */
  export let mode: 'whole' | 'cap' | 'nine' = 'whole'
  /** Corner size in art units (cap uses x only). */
  export let cx = 0
  export let cy = 0
  /** CSS px per art unit for `nine` corners. */
  export let s = 1
  /** Draw the glow outline too (hidden until `.lit` or a `.pressed` ancestor shows it). */
  export let glow = true
  /** Show the glow outline. */
  export let lit = false
  /** Draw only the part of the art inside this box (fractions 0..1: x0, y0, x1, y1). */
  export let part: [number, number, number, number] | null = null

  $: layers = glow && name !== 'hl_' ? [name, `hl_${name}`] : [name]
  $: [W, H] = artSize(name)
  $: clip = part
    ? `clip-path:inset(${part[1] * 100}% ${(1 - part[2]) * 100}% ${(1 - part[3]) * 100}% ${part[0] * 100}%)`
    : ''

  function cols(): Box[] {
    return [
      [0, 0, cx, H],
      [cx, 0, W - cx * 2, H],
      [W - cx, 0, cx, H],
    ]
  }
  function cells(): Box[][] {
    const xs: [number, number][] = [
      [0, cx],
      [cx, W - cx * 2],
      [W - cx, cx],
    ]
    const ys: [number, number][] = [
      [0, cy],
      [cy, H - cy * 2],
      [H - cy, cy],
    ]
    return ys.map(([y, h]) => xs.map(([x, w]) => [x, y, w, h] as Box))
  }
</script>

<span class="art m-{mode}" class:lit style="{clip};--cx:{cx * s}px;--cy:{cy * s}px" aria-hidden="true">
  {#each layers as layer, i}
    <span class="layer" class:hl={i === 1} class:row={mode === 'cap'}>
      {#if mode === 'whole'}
        {@html svg(layer)}
      {:else if mode === 'cap'}
        <span class="cap" style="aspect-ratio:{cx}/{H}">{@html svg(layer, cols()[0], 'none')}</span>
        <span class="mid">{@html svg(layer, cols()[1], 'none')}</span>
        <span class="cap" style="aspect-ratio:{cx}/{H}">{@html svg(layer, cols()[2], 'none')}</span>
      {:else}
        {#each cells() as row, r}
          {#each row as box, c}
            <span class="cell" class:edge={r === 1 || c === 1}>{@html svg(layer, box, 'none')}</span>
          {/each}
        {/each}
      {/if}
    </span>
  {/each}
</span>

<style>
  .art {
    position: absolute;
    inset: 0;
    display: block;
    pointer-events: none;
  }
  .layer {
    position: absolute;
    inset: 0;
  }
  .layer.hl {
    opacity: 0;
    transition: opacity 40ms;
  }
  .art.lit .layer.hl {
    opacity: 1;
  }
  .m-whole .layer :global(svg) {
    width: 100%;
    height: 100%;
  }
  .cap {
    display: block;
    flex: none;
    height: 100%;
  }
  .layer.row {
    display: flex;
  }
  .mid {
    flex: 1;
    height: 100%;
    margin: 0 -0.5px;
    position: relative;
    z-index: 0;
  }
  .cap {
    position: relative;
    z-index: 1;
  }
  .m-nine .layer {
    display: grid;
    grid-template-columns: var(--cx) 1fr var(--cx);
    grid-template-rows: var(--cy) 1fr var(--cy);
  }
  .cell {
    display: block;
    min-width: 0;
    min-height: 0;
    position: relative;
    z-index: 1;
  }
  .cell.edge {
    margin: -0.5px;
    z-index: 0;
  }
  .cap :global(svg),
  .mid :global(svg),
  .cell :global(svg) {
    display: block;
    width: 100%;
    height: 100%;
  }
</style>
