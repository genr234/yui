<script lang="ts">
  import { onMount } from 'svelte'
  import { bridge } from './sdk/bridge'

  type Section = 'home' | 'apps' | 'settings' | 'about'

  let open = false
  let section: Section = 'home'
  let status: any = null
  let diagnostics = ''
  let config: any = null
  let bridgeState = 'connecting'
  let pressTimer: number | undefined

  const sections: Array<{ id: Section; label: string }> = [
    { id: 'home', label: 'Home' },
    { id: 'apps', label: 'Apps' },
    { id: 'settings', label: 'Settings' },
    { id: 'about', label: 'About' },
  ]

  onMount(() => {
    void refresh()
  })

  async function refresh() {
    try {
      const [statusResult, diagnosticsResult, configResult] = await Promise.all([
        bridge.send<any>('status.get'),
        bridge.send<{ text: string }>('diagnostics.get'),
        bridge.send<any>('config.get'),
      ])
      status = statusResult
      diagnostics = diagnosticsResult?.text ?? ''
      config = configResult
      bridgeState = 'online'
    } catch (error) {
      bridgeState = 'offline'
      diagnostics = error instanceof Error ? error.message : String(error)
    }
  }

  function startPress() {
    clearPress()
    pressTimer = window.setTimeout(() => {
      open = true
      void refresh()
    }, 850)
  }

  function clearPress() {
    if (pressTimer) {
      window.clearTimeout(pressTimer)
      pressTimer = undefined
    }
  }

  async function reimportConfig() {
    await bridge.send('platform.reimport')
    await refresh()
  }

  async function selectChrome() {
    await bridge.send('platform.selectChrome')
    await refresh()
  }
</script>

<div
  class="hotspot"
  aria-label="Open Yui"
  role="button"
  tabindex="0"
  on:pointerdown={startPress}
  on:pointerup={clearPress}
  on:pointercancel={clearPress}
  on:pointerleave={clearPress}
  on:keydown={(event) => event.key === 'Enter' && (open = true)}
></div>

{#if open || localStorage.getItem('yui_always_open') === 'true'}
  <button class="veil" aria-label="Close Yui menu" on:click={() => (open = false)}></button>
  <section class="panel" aria-label="Yui Menu">
    <header class="header">
      <div>
        <h1>Yui</h1>
      </div>
    </header>

    <nav class="tabs" aria-label="Sections">
      {#each sections as item}
        <button class:active={section === item.id} on:click={() => (section = item.id)}>
          {item.label}
        </button>
      {/each}
    </nav>

    <main class="content">
      {#if section === 'home'}
        <h2>Controller Status</h2>
        <dl>
          <div><dt>Event</dt><dd>{status?.event ?? 'unknown'}</dd></div>
          <div><dt>Version</dt><dd>{status?.version ?? 'unknown'}</dd></div>
          <div><dt>Chrome</dt><dd>{status?.chrome_path ?? config?.chrome_path ?? 'unknown'}</dd></div>
          <div><dt>Restarts</dt><dd>{status?.restart_count ?? 0}</dd></div>
        </dl>
      {:else if section === 'apps'}
        <h2>Apps</h2>
        <div class="app-grid">
          <button on:click={refresh}>Refresh Status</button>
          <button on:click={reimportConfig}>Re-import Kiosk Config</button>
          <button on:click={selectChrome}>Select Chrome</button>
        </div>
      {:else if section === 'settings'}
        <h2>Settings</h2>
        <dl>
          <div><dt>Config</dt><dd>{config?.ConfigPath ?? config?.config_path ?? 'loaded'}</dd></div>
          <div><dt>HTTP</dt><dd>{config?.platform_http_addr ?? 'unknown'}</dd></div>
          <div><dt>Bridge</dt><dd>{config?.platform_bridge_addr ?? 'unknown'}</dd></div>
          <div><dt>Debug port</dt><dd>{config?.platform_remote_debugging_port ?? 'unknown'}</dd></div>
        </dl>
      {:else}
        <textarea class="diagnostics" readonly >{diagnostics || 'No diagnostics yet.'}</textarea>
      {/if}
    </main>

    <footer class="actions">
      <button on:click={refresh}>Refresh</button>
      <button class="primary" on:click={() => (open = false)}>Close</button>
    </footer>
  </section>
{/if}
