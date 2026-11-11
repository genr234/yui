<script lang="ts">
  // "Give my seat": the Home > Game row for a seated player.
  import type { Peer } from '@yui/protocol'
  import Avatar from './Avatar.svelte'
  import Icon from './Icon.svelte'
  import { colorHex } from './colors'
  import { resultText, type SeatState } from './seatPass'

  export let spectators: Peer[]
  export let seat: SeatState
  export let secondsLeft: number
  export let onOffer: (to: string) => void
  export let onCancel: () => void
  export let onDismissResult: () => void

  let picking = false
  $: if (seat.outgoing) picking = false
</script>

<section class="give" aria-label="Give my seat">
  {#if seat.outgoing}
    <p class="wait" role="status">Waiting for {seat.outgoing.to.name}… <span class="muted">{secondsLeft}s</span></p>
    <button class="secondary" type="button" on:click={onCancel}>Cancel</button>
  {:else if seat.result}
    <p class="wait" role="status">{resultText(seat.result)}</p>
    <button class="secondary" type="button" on:click={onDismissResult}>OK</button>
  {:else if picking}
    {#if spectators.length === 0}
      <p class="muted">Nobody is watching right now.</p>
    {:else}
      <ul>
        {#each spectators as s (s.id)}
          <li>
            <button class="spectator" type="button" style="--player:{colorHex(s.color)}" on:click={() => onOffer(s.id)}>
              <Avatar avatar={s.avatar ?? null} size={36} />
              <span>{s.name}</span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
    <button class="secondary" type="button" on:click={() => (picking = false)}>Back</button>
  {:else}
    <button class="secondary" type="button" on:click={() => (picking = true)}><Icon name="arrow_rotate" size={18} /> Give my seat</button>
  {/if}
</section>

<style>
  .give {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  p {
    margin: 0;
  }
  .wait {
    font-weight: 800;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 8px;
    max-height: 40vh;
    overflow-y: auto;
  }
  .spectator {
    width: 100%;
    min-height: 52px;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 12px;
    border: 3px solid var(--player);
    border-radius: 14px;
    background: #fff;
    color: inherit;
    font: inherit;
    font-weight: 800;
    text-align: left;
  }
</style>
