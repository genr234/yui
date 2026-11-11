<script lang="ts">
  import { mountAvatar, type AvatarProps } from '@yui/avatar'

  /** A creature, or from older pads an emoji or a selfie data URL. */
  export let avatar: string | null | undefined
  /** Edge in px. */
  export let size = 32
  /** The player's colour (a CSS colour); the creature's body and limbs take it. */
  export let color: string | undefined = undefined
  /** `face` fits a chip; `full` shows arms and legs and drops the round plate. */
  export let view: 'face' | 'full' = 'face'
  /** Keep the eyes still (rows of small previews). */
  export let still = false
  /** The white plate behind a creature's face. Turn it off on a light background. */
  export let plate = true

  // Everything inside is drawn by the shared renderer, as text or canvas, never HTML.
  function show(node: HTMLElement, props: AvatarProps) {
    const handle = mountAvatar(node, props)
    return { update: handle.update, destroy: handle.destroy }
  }
</script>

{#if avatar}
  <span class="avatar" class:full={view === 'full' || !plate} style="--size:{size}px" aria-hidden="true" use:show={{ avatar, color, view, still }}
  ></span>
{/if}

<style>
  .avatar {
    flex: 0 0 auto;
    box-sizing: border-box;
    width: var(--size);
    height: var(--size);
    display: inline-grid;
    place-items: center;
    overflow: hidden;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.22);
    line-height: 1;
    font-size: calc(var(--size) * 0.66);
  }
  /* A creature wears the player's colour, so it sits on a white plate to stay
     readable on a chip of that same colour. */
  .avatar:global([data-avatar-kind='creature']) {
    padding: calc(var(--size) * 0.1);
    background: #fff;
  }
  .avatar.full:global([data-avatar-kind='creature']) {
    padding: 0;
    overflow: visible;
    border-radius: 0;
    background: none;
  }
</style>
