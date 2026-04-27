<script lang="ts">
  import { sdk } from './sdk'

  let open = true
  let result = 'Click a button to exercise the boilerplate SDK.'

  const runStorageDemo = async () => {
    await sdk.storage.set('demo', 'hello from macOS')
    result = (await sdk.storage.get('demo')) ?? 'no value'
  }

  const runFsDemo = async () => {
    const files = await sdk.fs.list('/tmp')
    result = files.join(', ') || 'mock directory is empty'
  }
</script>

<style>
  :global(body) {
    margin: 0;
    font-family: sans-serif;
    background: #111827;
    color: #f9fafb;
  }

  .shell {
    position: fixed;
    top: 16px;
    right: 16px;
    width: 360px;
    padding: 16px;
    border-radius: 12px;
    background: rgba(17, 24, 39, 0.92);
    box-shadow: 0 12px 30px rgba(0, 0, 0, 0.35);
  }

  .row {
    display: flex;
    gap: 8px;
    margin-top: 12px;
  }

  button {
    border: 0;
    border-radius: 8px;
    padding: 10px 12px;
    cursor: pointer;
  }

  pre {
    white-space: pre-wrap;
    background: rgba(255, 255, 255, 0.06);
    border-radius: 8px;
    padding: 12px;
    margin-top: 12px;
  }
</style>

{#if open}
  <div class="shell">
    <h1>Kiosk Platform</h1>
    <p>Boilerplate overlay with mock SDK support for local macOS testing.</p>
    <div class="row">
      <button on:click={runStorageDemo}>Test storage</button>
      <button on:click={runFsDemo}>Test fs</button>
      <button on:click={() => (open = false)}>Hide</button>
    </div>
    <pre>{result}</pre>
  </div>
{:else}
  <button on:click={() => (open = true)}>Open platform</button>
{/if}
