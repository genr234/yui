<script lang="ts">
  import Icon from './Icon.svelte'
  import type { GuideEntry } from './layout/guide'

  export let entries: GuideEntry[]
  export let onClose: () => void
</script>

<div class="scrim" role="presentation" on:click|self={onClose}>
  <div class="sheet kit-panel" role="dialog" aria-modal="true" aria-label="Button guide">
    <header>
      <strong>What the buttons do</strong>
      <button class="close" type="button" aria-label="Close" on:click={onClose}><Icon name="cross" size={18} /></button>
    </header>
    <ul>
      {#each entries as e (e.id)}
        <li class:no-cap={!e.cap}>
          {#if e.cap}<span class="cap">{e.cap}</span>{/if}
          <span class="text">{e.text || e.cap}</span>
        </li>
      {/each}
    </ul>
    <button class="primary big" type="button" on:click={onClose}>Got it</button>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 20;
    display: flex;
    align-items: flex-end;
    justify-content: center;
    background: var(--yui-scrim);
  }
  .sheet {
    width: 100%;
    max-width: 480px;
    max-height: 100%;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 0 max(16px, env(safe-area-inset-right)) max(24px, env(safe-area-inset-bottom)) max(16px, env(safe-area-inset-left));
    border-bottom: 0;
    border-radius: var(--yui-radius-shell) var(--yui-radius-shell) 0 0;
    animation: up 220ms var(--yui-ease-out);
  }
  @keyframes up {
    from {
      transform: translateY(40px);
      opacity: 0;
    }
  }
  /* The player's coloured title strip, like the Home sheet's. */
  header {
    --edge: color-mix(in srgb, var(--player), #000 26%);
    position: sticky;
    top: 0;
    z-index: 1;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin: 0 calc(-1 * max(16px, env(safe-area-inset-left))) 4px;
    padding: 12px max(16px, env(safe-area-inset-left));
    border-bottom: 2px solid var(--edge);
    background: linear-gradient(180deg, color-mix(in srgb, var(--player), #fff 16%) 0 50%, var(--player) 50%);
    color: #fff;
    font-size: 1.2rem;
    text-shadow: 0 2px 0 var(--edge);
  }
  .close {
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    padding: 0;
    border: 2px solid var(--edge);
    border-radius: 50%;
    background: #fff;
    color: var(--ink);
    box-shadow: 0 3px 0 var(--edge);
    text-shadow: none;
  }
  .close:active {
    transform: translateY(3px);
    box-shadow: none;
  }
  ul {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 54px;
    padding: 6px 14px 6px 8px;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-hero);
    background: #fff;
    font-weight: 800;
  }
  li.no-cap {
    padding-left: 14px;
  }
  /* A little key cap, white like the neutral buttons. */
  .cap {
    flex: 0 0 auto;
    min-width: 42px;
    padding: 6px 8px;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-control);
    background: linear-gradient(180deg, #fff 0 50%, var(--kit-key) 50%);
    box-shadow: 0 3px 0 var(--kit-key-lip);
    text-align: center;
    font-weight: 900;
  }
  .text {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .big {
    min-height: 64px;
  }
</style>
