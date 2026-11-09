<script lang="ts">
  import { avatarImage } from './avatar'

  /** An emoji or a JPEG/WebP data URL; anything else is shown as text. */
  export let avatar: string | null | undefined
  /** Edge in px. */
  export let size = 32

  $: image = avatarImage(avatar)
</script>

{#if avatar}
  <span class="avatar" style="--size:{size}px" aria-hidden="true">
    {#if image}
      <img src={image} alt="" draggable="false" />
    {:else}
      <span class="emoji">{avatar}</span>
    {/if}
  </span>
{/if}

<style>
  .avatar {
    flex: 0 0 auto;
    width: var(--size);
    height: var(--size);
    display: inline-grid;
    place-items: center;
    overflow: hidden;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.22);
    line-height: 1;
  }
  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .emoji {
    font-size: calc(var(--size) * 0.66);
  }
</style>
