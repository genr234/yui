<script lang="ts">
  import { onDestroy } from 'svelte'
  import type { EmoteId, Peer, Player } from '@yui/protocol'
  import Avatar from './Avatar.svelte'
  import Icon from './Icon.svelte'
  import { colorHex } from './colors'
  import { EMOTES, EMOTE_LABELS, POKE_COOLDOWN_MS, REACT_COOLDOWN_MS } from './social'
  import { emoteUrl } from './emoteArt'

  export let player: Player
  /** Everyone else in the session (kiosk `players`). */
  export let peers: Peer[] = []
  /** The kiosk shows something private; reactions are dropped there. */
  export let sensitive = false
  export let onReact: (emote: EmoteId) => void
  export let onPoke: (to: string) => void
  export let onClose: () => void

  // Cooling keys: the controller drops a second reaction within 700 ms.
  let reactCool = false
  let lastEmote: EmoteId | '' = ''
  let reactTimer: ReturnType<typeof setTimeout> | null = null
  let pokeCool = false
  let pokedId = ''
  let pokeTimer: ReturnType<typeof setTimeout> | null = null

  function react(id: EmoteId) {
    if (reactCool || sensitive) return
    reactCool = true
    lastEmote = id
    onReact(id)
    reactTimer = setTimeout(() => {
      reactCool = false
      lastEmote = ''
    }, REACT_COOLDOWN_MS)
  }
  function poke(id: string) {
    if (pokeCool || sensitive) return
    pokeCool = true
    pokedId = id
    onPoke(id)
    pokeTimer = setTimeout(() => {
      pokeCool = false
      pokedId = ''
    }, POKE_COOLDOWN_MS)
  }
  onDestroy(() => {
    if (reactTimer) clearTimeout(reactTimer)
    if (pokeTimer) clearTimeout(pokeTimer)
  })

  $: pokable = peers.filter((p) => p.pokes)
</script>

<div class="scrim" role="presentation" on:click|self={onClose}>
  <div class="sheet kit-panel" role="dialog" aria-modal="true" aria-label="Reactions" style="--player:{colorHex(player.color)};--cool:{REACT_COOLDOWN_MS}ms;--pcool:{POKE_COOLDOWN_MS}ms">
    <header>
      <strong>React</strong>
      <button class="close" type="button" aria-label="Close" on:click={onClose}><Icon name="cross" size={18} /></button>
    </header>

    {#if sensitive}
      <p class="note" role="status">The kiosk is showing something private. Reactions are off for now.</p>
    {/if}

    <div class="grid" class:cooling={reactCool}>
      {#each EMOTES as id (id)}
        <button
          class="key"
          class:sent={lastEmote === id}
          type="button"
          aria-label={EMOTE_LABELS[id]}
          disabled={sensitive}
          on:click={() => react(id)}
        >
          <span class="tile"><img src={emoteUrl(id)} alt="" width="32" height="38" draggable="false" /></span>
        </button>
      {/each}
    </div>

    <h2>Poke someone</h2>
    {#if pokable.length === 0}
      <p class="note">{peers.length === 0 ? 'Nobody else is here yet.' : 'Nobody here is taking pokes.'}</p>
    {:else}
      <ul class="people">
        {#each pokable as p (p.id)}
          <li>
            <button
              class="person"
              class:sent={pokedId === p.id}
              type="button"
              style="--c:{colorHex(p.color)}"
              disabled={sensitive}
              aria-disabled={pokeCool}
              on:click={() => poke(p.id)}
            >
              <span class="who"
                ><span class="slot">{p.avatar ? '' : p.slot ? `P${p.slot}` : '·'}<Avatar avatar={p.avatar} size={30} /></span><span class="nm">{p.name}</span></span
              >
              <span class="act">{pokedId === p.id ? 'Poked!' : 'Poke'}</span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
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
  h2 {
    margin: 4px 0 0;
    font-size: 1rem;
    font-weight: 900;
  }
  .note {
    margin: 0;
    padding: 10px 14px;
    border-radius: var(--yui-radius-hero);
    background: var(--kit-well);
    font-weight: 700;
    color: var(--ink-muted);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 10px;
  }
  /* A big white key with the balloon on a sky tile; a cooldown dims every key,
     and the one just sent fills back up over the 700 ms. */
  .key {
    position: relative;
    display: grid;
    place-items: center;
    padding: 6px;
    min-height: 88px;
    overflow: hidden;
    border: 2px solid var(--kit-key-line);
    border-radius: var(--yui-radius-hero);
    background: linear-gradient(180deg, #fff 0 50%, var(--kit-key) 50%);
    box-shadow: 0 var(--yui-lip) 0 var(--kit-key-lip);
    transition: transform var(--yui-press) var(--yui-ease-out), box-shadow var(--yui-press) var(--yui-ease-out), opacity 120ms;
  }
  .key:active:not(:disabled) {
    transform: translateY(var(--yui-lip));
    box-shadow: 0 0 0 var(--kit-key-lip);
  }
  .key:disabled {
    opacity: 0.5;
  }
  .grid.cooling .key:not(.sent) {
    opacity: 0.55;
  }
  .key.sent::after {
    content: '';
    position: absolute;
    inset: 0;
    background: rgba(255, 255, 255, 0.65);
    transform-origin: top;
    animation: drain var(--cool) linear forwards;
  }
  @keyframes drain {
    from {
      transform: scaleY(1);
    }
    to {
      transform: scaleY(0);
    }
  }
  .tile {
    display: grid;
    place-items: center;
    width: 100%;
    height: 76px;
    border-radius: var(--yui-radius-control);
    background: var(--kit-sky);
    box-shadow: inset 0 3px 0 rgba(10, 50, 90, 0.18);
  }
  /* Kenney's vector balloons: crisp at any size. */
  img {
    width: 56px;
    height: 66px;
  }
  .people {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .person {
    --edge: color-mix(in srgb, var(--c), #000 26%);
    position: relative;
    width: 100%;
    min-height: 58px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 6px 8px 6px 6px;
    overflow: hidden;
    border: 2px solid var(--kit-key-line);
    border-left: 8px solid var(--c);
    border-radius: var(--yui-radius-hero);
    background: #fff;
    box-shadow: 0 3px 0 var(--kit-key-lip);
    font-weight: 800;
    text-align: left;
    transition: transform var(--yui-press) var(--yui-ease-out), box-shadow var(--yui-press) var(--yui-ease-out);
  }
  .person:active:not(:disabled) {
    transform: translateY(3px);
    box-shadow: 0 0 0 var(--kit-key-lip);
  }
  .person[aria-disabled='true']:not(.sent) {
    opacity: 0.55;
  }
  .person.sent::after {
    content: '';
    position: absolute;
    inset: 0;
    background: rgba(255, 255, 255, 0.5);
    transform-origin: left;
    animation: pdrain var(--pcool) linear forwards;
  }
  @keyframes pdrain {
    from {
      transform: scaleX(1);
    }
    to {
      transform: scaleX(0);
    }
  }
  .who {
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .slot {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 30px;
    font-weight: 900;
    color: var(--edge);
  }
  .nm {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .act {
    flex: 0 0 auto;
    padding: 6px 14px;
    border-radius: var(--yui-radius-control);
    background: var(--c);
    color: #fff;
    text-shadow: 0 2px 0 var(--edge);
    box-shadow: 0 3px 0 var(--edge);
  }
</style>
