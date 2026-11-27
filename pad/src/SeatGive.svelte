<script lang="ts">
  // "Give my seat": the Home > Game row for a seated player.
  import type { Peer } from '@yui/protocol'
  import Avatar from './Avatar.svelte'
  import SettingIcon from './SettingIcon.svelte'
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

<section class="set-section" aria-label="Give my seat">
  <h3 class="set-title">Seat</h3>
  <div class="set-group">
    {#if seat.outgoing}
      <div class="set-row" role="status">
        <span class="set-badge"><SettingIcon name="swap" /></span>
        <span class="set-label"><span>Waiting for {seat.outgoing.to.name}…</span></span>
        <span class="set-value">{secondsLeft}s</span>
      </div>
      <button class="set-row" type="button" on:click={onCancel}>
        <span class="set-badge"><SettingIcon name="chevron" /></span>
        <span class="set-label"><span>Cancel</span></span>
      </button>
    {:else if seat.result}
      <div class="set-row" role="status">
        <span class="set-badge"><SettingIcon name="swap" /></span>
        <span class="set-label"><span>{resultText(seat.result)}</span></span>
      </div>
      <button class="set-row" type="button" on:click={onDismissResult}>
        <span class="set-badge"><SettingIcon name="chevron" /></span>
        <span class="set-label"><span>OK</span></span>
      </button>
    {:else if picking}
      {#if spectators.length === 0}
        <div class="set-row">
          <span class="set-badge"><SettingIcon name="user" /></span>
          <span class="set-label"><span class="set-sub">Nobody is watching right now.</span></span>
        </div>
      {:else}
        <ul>
          {#each spectators as s (s.id)}
            <li>
              <button class="set-row" type="button" on:click={() => onOffer(s.id)}>
                <Avatar avatar={s.avatar ?? null} color={colorHex(s.color)} size={36} />
                <span class="set-label"><span>{s.name}</span></span>
                <span class="set-chevron"><SettingIcon name="chevron" size={18} /></span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
      <button class="set-row" type="button" on:click={() => (picking = false)}>
        <span class="set-badge back"><SettingIcon name="chevron" /></span>
        <span class="set-label"><span>Back</span></span>
      </button>
    {:else}
      <button class="set-row" type="button" on:click={() => (picking = true)}>
        <span class="set-badge"><SettingIcon name="swap" /></span>
        <span class="set-label"><span>Give my seat</span></span>
        <span class="set-chevron"><SettingIcon name="chevron" size={18} /></span>
      </button>
    {/if}
  </div>
</section>

<style>
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 40vh;
    overflow-y: auto;
  }
  /* Rows in the list and the row after it keep their hairlines. */
  li + li :global(.set-row)::before,
  ul + :global(.set-row)::before {
    content: '';
    position: absolute;
    top: 0;
    left: 66px;
    right: 16px;
    border-top: 1.5px solid #e9ecf1;
  }
  .back {
    transform: scaleX(-1);
  }
</style>
