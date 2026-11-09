<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import Avatar from './Avatar.svelte'
  import {
    AVATAR_EMOJIS,
    avatarImage,
    cameraErrorMessage,
    cameraSupported,
    captureSelfie,
    openFrontCamera,
    stopStream,
  } from './avatar'

  /** The chosen avatar: an emoji, a selfie data URL, or null for none. */
  export let value: string | null = null
  export let disabled = false

  let stream: MediaStream | null = null
  let video: HTMLVideoElement | undefined
  let starting = false
  let message = ''
  const canSelfie = cameraSupported()

  $: selfie = avatarImage(value)

  function pick(emoji: string) {
    value = value === emoji ? null : emoji
    message = ''
  }

  async function startCamera() {
    message = ''
    starting = true
    try {
      const s = await openFrontCamera()
      if (!starting) {
        stopStream(s) // cancelled while the prompt was up
        return
      }
      stream = s
      await tick()
      if (video) {
        video.srcObject = s
        await video.play().catch(() => {})
      }
    } catch (e) {
      message = cameraErrorMessage(e)
      closeCamera()
    } finally {
      starting = false
    }
  }

  function capture() {
    const url = video ? captureSelfie(video) : null
    if (url) value = url
    else message = "Couldn't take the photo. Try again or pick an emoji."
    closeCamera()
  }

  function closeCamera() {
    starting = false
    stopStream(stream)
    stream = null
    if (video) video.srcObject = null
  }

  onDestroy(closeCamera)
</script>

<div class="picker">
  {#if stream || starting}
    <div class="camera">
      <div class="preview">
        <!-- svelte-ignore a11y-media-has-caption -->
        <video bind:this={video} autoplay playsinline muted></video>
      </div>
      <div class="camera-actions">
        <button type="button" class="secondary" on:click={closeCamera}>Cancel</button>
        <button type="button" class="primary shutter" disabled={!stream} on:click={capture}>
          {stream ? 'Take photo' : 'Starting camera…'}
        </button>
      </div>
    </div>
  {:else}
    <div class="emojis" role="radiogroup" aria-label="Avatar">
      {#each AVATAR_EMOJIS as emoji}
        <button
          type="button"
          role="radio"
          aria-checked={value === emoji}
          aria-label={emoji}
          class="emoji"
          class:selected={value === emoji}
          {disabled}
          on:click={() => pick(emoji)}>{emoji}</button
        >
      {/each}
    </div>
    {#if canSelfie}
      <div class="selfie-row">
        {#if selfie}
          <Avatar avatar={selfie} size={48} />
        {/if}
        <button type="button" class="secondary" {disabled} on:click={startCamera}>
          {selfie ? 'Retake selfie' : 'Take a selfie'}
        </button>
        {#if selfie}
          <button type="button" class="link" {disabled} on:click={() => (value = null)}>Remove</button>
        {/if}
      </div>
    {/if}
  {/if}
  {#if message}<p class="note" role="status">{message}</p>{/if}
</div>

<style>
  .picker {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .emojis {
    display: grid;
    grid-template-columns: repeat(6, minmax(0, 1fr));
    gap: 8px;
  }
  .emoji {
    aspect-ratio: 1;
    padding: 0;
    border: 3px solid transparent;
    border-radius: 50%;
    background: var(--yui-chrome-fill);
    font-size: 1.5rem;
    line-height: 1;
  }
  .emoji.selected {
    border-color: #fff;
    background: var(--yui-chrome-active);
  }
  .selfie-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .selfie-row .secondary {
    flex: 1;
  }
  .link {
    border: 0;
    background: none;
    color: var(--muted);
    font: inherit;
    font-weight: 700;
    text-decoration: underline;
  }
  .camera {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
  }
  .preview {
    width: 200px;
    height: 200px;
    border-radius: 50%;
    overflow: hidden;
    background: #000;
    box-shadow: 0 0 0 4px var(--player, var(--accent)), 0 16px 40px rgba(0, 0, 0, 0.4);
  }
  video {
    width: 100%;
    height: 100%;
    object-fit: cover;
    transform: scaleX(-1);
  }
  .camera-actions {
    display: grid;
    grid-template-columns: 1fr 1.4fr;
    gap: 10px;
    width: 100%;
  }
  .note {
    margin: 0;
    color: var(--muted);
    font-size: 0.9rem;
    font-weight: 600;
  }
</style>
