<script lang="ts">
  /**
   * "Install Yui Pad": playing from the home-screen app is full screen, with
   * no address bar or tabs to swipe into mid-game. Android gets the browser's
   * install dialog; iOS has none, so it gets the two taps to do instead.
   */
  import { installState, promptInstall } from './platform/pwa'

  let iosOpen = false
</script>

{#if $installState === 'prompt'}
  <div class="hint kit-panel">
    <span><strong>Play full screen</strong><small>Install Yui Pad: no browser bars mid-game.</small></span>
    <button class="primary small" type="button" on:click={() => void promptInstall()}>Install</button>
  </div>
{:else if $installState === 'ios'}
  <div class="hint kit-panel">
    {#if iosOpen}
      <ol>
        <li>Tap the <strong>Share</strong> button in Safari's toolbar (the square with an arrow).</li>
        <li>Scroll down and tap <strong>Add to Home Screen</strong>, then <strong>Add</strong>.</li>
        <li>Open <strong>Yui Pad</strong> from your home screen next time.</li>
      </ol>
      <button class="secondary small" type="button" on:click={() => (iosOpen = false)}>Got it</button>
    {:else}
      <span><strong>Play full screen</strong><small>Add Yui Pad to your home screen.</small></span>
      <button class="secondary small" type="button" on:click={() => (iosOpen = true)}>How</button>
    {/if}
  </div>
{/if}

<style>
  .hint {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    width: 100%;
    padding: 10px 12px;
    color: var(--ink);
    text-align: left;
  }
  span {
    display: grid;
    gap: 2px;
  }
  small {
    color: var(--ink-muted);
  }
  ol {
    margin: 0;
    padding-left: 20px;
    line-height: 1.4;
  }
  .small {
    min-height: 40px;
    padding: 6px 16px;
  }
</style>
