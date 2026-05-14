<script lang="ts">
  import { createEventDispatcher } from "svelte";
  import { onDestroy, onMount } from "svelte";
  import { tweened } from "svelte/motion";
  import { cubicOut } from "svelte/easing";
  import ArrowLeftIcon from "lucide-svelte/icons/arrow-left";
  import CheckCircleIcon from "lucide-svelte/icons/check-circle-2";
  import ChevronRightIcon from "lucide-svelte/icons/chevron-right";
  import DownloadIcon from "lucide-svelte/icons/download";
  import PlayIcon from "lucide-svelte/icons/play";
  import PlusIcon from "lucide-svelte/icons/plus";
  import RefreshCwIcon from "lucide-svelte/icons/refresh-cw";
  import SettingsIcon from "lucide-svelte/icons/settings";
  import TrashIcon from "lucide-svelte/icons/trash-2";
  import XCircleIcon from "lucide-svelte/icons/x-circle";
  import PermissionCards from "../components/PermissionCards.svelte";
  import {
    describePluginPermission,
    clearPluginStorage,
    isAdministratorPermission,
    listPluginStorageKeys,
    pluginCatalog,
    pluginCatalogEntryId,
    pluginSources,
    plugins,
    type YuiPlugin,
    type YuiPluginCatalogEntry,
    type YuiPluginLog,
    type YuiPluginSource,
  } from "../sdk/plugins";
  import EmptyState from "../components/EmptyState.svelte";

  export let mode: "toggle" | "manage" = "toggle";
  export let embedded = false;
  export let hideNav = false;
  export let openPluginRequest: { pluginId: string; nonce: number } | null =
    null;

  const dispatch = createEventDispatcher<{
    page: { title: string; canGoBack: boolean; back: () => void };
    settings: { pluginId: string };
    store: void;
  }>();
  const gaugeTicks = Array.from(
    { length: 9 },
    (_, index) => 200 + index * 17.5,
  );
  const needleAngle = tweened(200, { duration: 420, easing: cubicOut });

  let installed: YuiPlugin[] = [];
  let sources: YuiPluginSource[] = [];
  let catalog: YuiPluginCatalogEntry[] = [];
  let selectedId = "";
  let page: "plugins" | "plugin" | "sources" = "plugins";
  let loading = true;
  let busy = "";
  let error = "";
  let message = "";
  let sourceURL = "";
  let settingsDraft: Record<string, unknown> = {};
  let secretDraft: Record<string, string> = {};
  let logs: YuiPluginLog[] = [];
  let storageKeys: string[] = [];
  let runOutput = "";
  let pendingPluginInstall: YuiPluginCatalogEntry | null = null;
  let pendingPluginPermissions: string[] = [];
  let gaugeInitialized = false;
  let consumedOpenPluginNonce = 0;
  let refreshTimer: number | undefined;
  let detailPollTimer: number | undefined;
  let detailPollKey = "";

  onMount(() => {
    void loadAll();
    const refresh = (event: Event) => {
      window.clearTimeout(refreshTimer);
      refreshTimer = window.setTimeout(() => {
        void refreshAfterPluginChange(
          (event as CustomEvent<{ pluginId?: string }>).detail?.pluginId,
        );
      }, 40);
    };
    window.addEventListener("yui:plugins-changed", refresh);
    return () => {
      window.clearTimeout(refreshTimer);
      window.clearInterval(detailPollTimer);
      window.removeEventListener("yui:plugins-changed", refresh);
    };
  });

  onDestroy(() => {
    window.clearTimeout(refreshTimer);
    window.clearInterval(detailPollTimer);
  });

  $: selected =
    installed.find((plugin) => plugin.id === selectedId) ?? installed[0];
  $: installedIds = new Set(
    installed.filter((plugin) => plugin.installed).map((plugin) => plugin.id),
  );
  $: selectedPermissions = selected?.plugin.permissions ?? [];
  $: selectedAdministratorPermissions = selectedPermissions.filter(
    isAdministratorPermission,
  );
  $: selectedSettings = Object.entries(selected?.plugin.settings ?? {});
  $: runningPlugins = installed.filter((plugin) => plugin.enabled).length;
  $: erroredPlugins = installed.filter((plugin) => plugin.lastError).length;
  $: pluginRunRatio = installed.length ? runningPlugins / installed.length : 0;
  $: pluginErrorRatio = installed.length
    ? erroredPlugins / installed.length
    : 0;
  $: pluginNeedleTarget = 200 + pluginRunRatio * 140;
  $: if (!loading) {
    needleAngle.set(pluginNeedleTarget, {
      duration: gaugeInitialized ? 420 : 0,
      easing: cubicOut,
    });
    gaugeInitialized = true;
  }
  $: pluginNeedleRadians = ($needleAngle * Math.PI) / 180;
  $: pluginNeedleX = 130 + 72 * Math.cos(pluginNeedleRadians);
  $: pluginNeedleY = 100 + 72 * Math.sin(pluginNeedleRadians);
  $: pluginHealthLabel =
    installed.length === 0
      ? "No plugins"
      : erroredPlugins > 0
        ? erroredPlugins === 1
          ? "1 plugin needs attention"
          : `${erroredPlugins} plugins need attention`
        : runningPlugins === installed.length
          ? "All running"
          : runningPlugins === 0
            ? "All stopped"
            : `${runningPlugins} of ${installed.length} running`;
  $: pluginGaugeTone =
    pluginErrorRatio >= 0.5
      ? "danger"
      : pluginErrorRatio > 0
        ? "warning"
        : "healthy";
  $: if (
    mode === "manage" &&
    openPluginRequest &&
    openPluginRequest.nonce !== consumedOpenPluginNonce &&
    installed.length > 0
  ) {
    consumedOpenPluginNonce = openPluginRequest.nonce;
    openPluginFromRequest(openPluginRequest.pluginId);
  }
  $: dispatch("page", {
    title:
      page === "sources"
        ? "Plugin sources"
        : page === "plugin" && selected
          ? selected.name
          : "Plugins",
    canGoBack: page !== "plugins",
    back: () => (page = "plugins"),
  });
  $: syncDetailPolling(page === "plugin" ? (selected?.id ?? "") : "");

  async function loadAll() {
    loading = true;
    error = "";
    try {
      [installed, sources, catalog] = await Promise.all([
        plugins.list(),
        pluginSources.list().catch(() => []),
        pluginCatalog.list().catch(() => []),
      ]);
      if (
        !selectedId ||
        !installed.some((plugin) => plugin.id === selectedId)
      ) {
        selectedId = installed[0]?.id ?? "";
      }
      if (
        openPluginRequest &&
        openPluginRequest.nonce !== consumedOpenPluginNonce
      ) {
        consumedOpenPluginNonce = openPluginRequest.nonce;
        openPluginFromRequest(openPluginRequest.pluginId);
      }
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  async function openPlugin(plugin: YuiPlugin) {
    selectedId = plugin.id;
    page = "plugin";
    runOutput = "";
    await loadPluginDetails(plugin.id);
  }

  function openPluginFromRequest(pluginId: string) {
    if (!pluginId) return;
    const plugin = installed.find((item) => item.id === pluginId);
    if (!plugin) return;
    void openPlugin(plugin);
  }

  async function loadPluginDetails(id: string) {
    try {
      const [settings, audit, keys] = await Promise.all([
        plugins.getSettings(id).catch(() => ({})),
        plugins.logs(id).catch(() => []),
        listPluginStorageKeys(id).catch(() => []),
      ]);
      settingsDraft = { ...settings };
      secretDraft = {};
      logs = audit;
      storageKeys = keys;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  function syncDetailPolling(pluginId: string) {
    if (pluginId === detailPollKey) return;
    detailPollKey = pluginId;
    window.clearInterval(detailPollTimer);
    detailPollTimer = undefined;
    if (!pluginId) return;
    detailPollTimer = window.setInterval(() => {
      void loadPluginDetails(pluginId);
    }, 5000);
  }

  async function refreshAfterPluginChange(pluginId?: string) {
    await loadAll();
    if (
      page === "plugin" &&
      selected?.id &&
      (!pluginId || pluginId === selected.id)
    ) {
      await loadPluginDetails(selected.id);
    }
  }

  async function togglePlugin(plugin: YuiPlugin, enabled: boolean) {
    busy = `toggle:${plugin.id}`;
    error = "";
    message = "";
    try {
      if (enabled) await plugins.enable(plugin.id);
      else await plugins.disable(plugin.id);
      message = enabled
        ? `${plugin.name} enabled.`
        : `${plugin.name} disabled.`;
      await loadAll();
      if (page === "plugin") await loadPluginDetails(plugin.id);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = "";
    }
  }

  async function togglePermission(permission: string, checked: boolean) {
    if (!selected) return;
    const next = new Set(selected.grantedPermissions ?? []);
    if (checked) next.add(permission);
    else next.delete(permission);
    busy = `permission:${permission}`;
    error = "";
    try {
      await plugins.updatePermissions(selected.id, [...next]);
      await loadAll();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = "";
    }
  }

  async function toggleAdministrator(trusted: boolean) {
    if (!selected) return;
    busy = "administrator";
    error = "";
    message = "";
    try {
      await plugins.updateAdministrator(selected.id, trusted);
      message = trusted
        ? "Administrator access granted."
        : "Administrator access revoked.";
      await loadAll();
      if (page === "plugin") await loadPluginDetails(selected.id);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = "";
    }
  }

  async function saveSettings() {
    if (!selected) return;
    busy = "settings";
    error = "";
    message = "";
    try {
      settingsDraft = await plugins.updateSettings(
        selected.id,
        settingsDraft,
        secretDraft,
      );
      secretDraft = {};
      message = "Plugin settings saved.";
      await loadPluginDetails(selected.id);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = "";
    }
  }

  async function clearSelectedPluginStorage() {
    if (!selected) return;
    busy = "storage";
    error = "";
    message = "";
    try {
      await clearPluginStorage(selected.id);
      storageKeys = [];
      message = "Plugin storage cleared.";
      await loadPluginDetails(selected.id);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = "";
    }
  }

  async function addSource() {
    const url = sourceURL.trim();
    if (!url) return;
    busy = "source:add";
    error = "";
    message = "";
    try {
      await pluginSources.add(url);
      sourceURL = "";
      message = "Plugin source added and refreshed.";
      await loadAll();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      await loadAll();
    } finally {
      busy = "";
    }
  }

  async function refreshSource(id: string) {
    busy = `source:refresh:${id}`;
    error = "";
    message = "";
    try {
      await pluginSources.refresh(id);
      message = "Plugin source refreshed.";
      await loadAll();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      await loadAll();
    } finally {
      busy = "";
    }
  }

  async function removeSource(id: string) {
    busy = `source:remove:${id}`;
    error = "";
    message = "";
    try {
      await pluginSources.remove(id);
      message = "Plugin source removed.";
      await loadAll();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = "";
    }
  }

  async function install(entry: YuiPluginCatalogEntry) {
    busy = `install:${pluginCatalogEntryId(entry)}`;
    error = "";
    message = "";
    try {
      await pluginCatalog.install(pluginCatalogEntryId(entry));
      await plugins.updatePermissions(
        entry.plugin.id,
        pendingPluginPermissions,
      );
      message = `${entry.plugin.name} installed.`;
      await loadAll();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = "";
    }
  }

  async function uninstall(id: string) {
    busy = `uninstall:${id}`;
    error = "";
    message = "";
    try {
      await plugins.uninstall(id);
      message = "Plugin uninstalled.";
      page = "plugins";
      await loadAll();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = "";
    }
  }

  async function runCommand(commandId: string) {
    if (!selected) return;
    busy = `run:${commandId}`;
    error = "";
    runOutput = "";
    try {
      const result = await plugins.run(selected.id, commandId);
      runOutput = JSON.stringify(result, null, 2);
      await loadPluginDetails(selected.id);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      await loadPluginDetails(selected.id);
    } finally {
      busy = "";
    }
  }

  function setSetting(key: string, value: unknown) {
    settingsDraft = { ...settingsDraft, [key]: value };
  }

  function setSecret(key: string, value: string) {
    secretDraft = { ...secretDraft, [key]: value };
  }

  function granted(permission: string) {
    return selected?.grantedPermissions?.includes(permission) ?? false;
  }

  async function setAllSelectedPermissions(allowed: boolean) {
    if (!selected) return;
    busy = "permissions:all";
    error = "";
    try {
      await plugins.updatePermissions(
        selected.id,
        allowed ? selectedPermissions : [],
      );
      await loadAll();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = "";
    }
  }

  function startPluginInstall(entry: YuiPluginCatalogEntry) {
    pendingPluginInstall = entry;
    pendingPluginPermissions = [...(entry.plugin.permissions ?? [])];
  }

  function togglePendingPluginPermission(permission: string, granted: boolean) {
    const next = new Set(pendingPluginPermissions);
    if (granted) next.add(permission);
    else next.delete(permission);
    pendingPluginPermissions = [...next];
  }

  function pluginSubtitle(plugin: YuiPlugin) {
    const kind = plugin.dev
      ? "local"
      : plugin.installed
        ? "installed"
        : "available";
    return `${plugin.id} · ${plugin.version} · ${kind}${plugin.administratorTrusted ? " · admin" : ""}`;
  }
</script>

{#if loading}
  <EmptyState
    title="Loading plugins"
    body="Checking local and installed Starlark plugins."
  />
{:else if error && installed.length === 0}
  <EmptyState title="Plugin load failed" body={error} />
{:else if mode === "toggle"}
  <section
    class:settings-stack={!embedded}
    class:plugin-manager-shell={embedded}
  >
    <div class:settings-page={!embedded} class:plugin-manager-page={embedded}>
      {#if error}
        <section class="settings-group">
          <div class="settings-row static">
            <span>
              <span>{"Plugin error"}</span>
              <small>{error}</small>
            </span>
          </div>
        </section>
      {/if}

      {#if installed.length === 0}
        <section class="empty-with-action">
          <EmptyState
            title="No plugins installed"
            body="Add signed plugins from trusted catalogs."
          />
        </section>
      {:else}
        <section
          class={`plugin-health-gauge ${pluginGaugeTone}`}
          aria-label="Plugin health"
        >
          <svg
            class="plugin-gauge-svg"
            viewBox="0 0 260 116"
            aria-hidden="true"
          >
            <path
              class="plugin-gauge-outer"
              d="M 32 102 A 98 98 0 0 1 228 102"
            />
            <path
              class="plugin-gauge-inner"
              d="M 50 102 A 80 80 0 0 1 210 102"
            />
            <path
              class="plugin-gauge-band"
              d="M 44 102 A 86 86 0 0 1 216 102"
            />
            {#each gaugeTicks as angle}
              <line
                class="plugin-gauge-tick"
                x1={130 + 80 * Math.cos((angle * Math.PI) / 180)}
                y1={100 + 80 * Math.sin((angle * Math.PI) / 180)}
                x2={130 + 93 * Math.cos((angle * Math.PI) / 180)}
                y2={100 + 93 * Math.sin((angle * Math.PI) / 180)}
              />
            {/each}
            <line
              class="plugin-gauge-needle"
              x1="130"
              y1="100"
              x2={pluginNeedleX}
              y2={pluginNeedleY}
            />
            <circle class="plugin-gauge-hub-ring" cx="130" cy="100" r="15" />
            <circle class="plugin-gauge-hub" cx="130" cy="100" r="11" />
          </svg>
          <div class="plugin-gauge-copy">
            <strong>{pluginHealthLabel}</strong>
            <small>
              {runningPlugins}/{installed.length} running · {erroredPlugins} errors
            </small>
          </div>
        </section>

        <section class="settings-group">
          {#each installed as plugin}
            <div class="settings-row permission-row plugin-toggle-row">
              <span>
                <span>{plugin.name}</span>
                <small
                  >{pluginSubtitle(plugin)}{plugin.lastError
                    ? ` · ${plugin.lastError}`
                    : ""}</small
                >
              </span>
              <span class="settings-actions">
                <button
                  class="icon-button compact"
                  aria-label={`Open ${plugin.name} settings`}
                  title="Plugin settings"
                  type="button"
                  on:click={() => dispatch("settings", { pluginId: plugin.id })}
                >
                  <SettingsIcon size={16} strokeWidth={2.4} />
                </button>
                <input
                  type="checkbox"
                  checked={plugin.enabled}
                  disabled={Boolean(busy)}
                  on:change={(event) =>
                    togglePlugin(plugin, event.currentTarget.checked)}
                />
              </span>
            </div>
          {/each}
        </section>
      {/if}
    </div>
  </section>
{:else if page === "sources"}
  <section
    class:settings-stack={!embedded}
    class:plugin-manager-shell={embedded}
  >
    <div class:settings-page={!embedded} class:plugin-manager-page={embedded}>
      {#if !hideNav}
        <nav class="settings-nav" aria-label="Plugin source navigation">
          <button
            class="icon-button"
            aria-label="Back to plugins"
            title="Back to plugins"
            on:click={() => (page = "plugins")}
          >
            <ArrowLeftIcon size={18} strokeWidth={2.4} />
          </button>
          <span>Plugin sources</span>
        </nav>
      {/if}

      <section class="settings-group">
        <div class="settings-row static settings-actions-row">
          <input
            class="settings-text-input"
            placeholder="https://example.com/yui/plugin-catalog.json"
            bind:value={sourceURL}
            disabled={Boolean(busy)}
          />
          <button
            class="settings-inline-button"
            disabled={Boolean(busy) || !sourceURL.trim()}
            title="Add plugin source"
            on:click={addSource}
          >
            <PlusIcon size={14} strokeWidth={2.4} />
            Add
          </button>
        </div>
        {#if error || message}
          <div class="settings-row static">
            <span>
              <span>{error ? "Source error" : "Source status"}</span>
              <small>{error || message}</small>
            </span>
          </div>
        {/if}
      </section>

      <section class="settings-group">
        {#if sources.length === 0}
          <div class="settings-row static">
            <span>
              <span>No sources</span>
              <small
                >Add a signed HTTPS plugin catalog to discover plugins.</small
              >
            </span>
          </div>
        {:else}
          {#each sources as source}
            <div class="settings-row static settings-actions-row">
              <span>
                <span>{source.name || source.url}</span>
                <small
                  >{source.lastStatus}{source.publisher
                    ? ` · ${source.publisher}`
                    : ""}{source.lastError
                    ? ` · ${source.lastError}`
                    : ""}</small
                >
              </span>
              <span class="settings-actions">
                <button
                  class="settings-inline-button"
                  disabled={Boolean(busy)}
                  title="Refresh source"
                  on:click={() => refreshSource(source.id)}
                >
                  <RefreshCwIcon size={14} strokeWidth={2.4} />
                  Refresh
                </button>
                <button
                  class="settings-inline-button danger"
                  disabled={Boolean(busy)}
                  title="Remove source"
                  on:click={() => removeSource(source.id)}
                >
                  <TrashIcon size={14} strokeWidth={2.4} />
                  Remove
                </button>
              </span>
            </div>
          {/each}
        {/if}
      </section>

      <section class="settings-group">
        {#if catalog.length === 0}
          <div class="settings-row static">
            <span>
              <span>No catalog plugins</span>
              <small>Refresh a source to load signed plugin listings.</small>
            </span>
          </div>
        {:else}
          {#each catalog as entry}
            <div class="settings-row static settings-actions-row">
              <span>
                <span>{entry.plugin.name}</span>
                <small
                  >{entry.plugin.id} · {entry.plugin.version} · {entry.catalog}{entry
                    .plugin.permissions?.length
                    ? ` · ${entry.plugin.permissions.join(", ")}`
                    : " · no permissions"}</small
                >
              </span>
              {#if installedIds.has(entry.plugin.id)}
                <span class="settings-inline-button" aria-disabled="true"
                  >Installed</span
                >
              {:else}
                <button
                  class="settings-inline-button"
                  disabled={Boolean(busy)}
                  title="Install signed plugin"
                  on:click={() => startPluginInstall(entry)}
                >
                  <DownloadIcon size={14} strokeWidth={2.4} />
                  Install
                </button>
              {/if}
            </div>
          {/each}
        {/if}
      </section>
    </div>
  </section>
{:else if page === "plugin" && selected}
  <section
    class:settings-stack={!embedded}
    class:plugin-manager-shell={embedded}
  >
    <div class:settings-page={!embedded} class:plugin-manager-page={embedded}>
      {#if !hideNav}
        <nav class="settings-nav" aria-label="Plugin navigation">
          <button
            class="icon-button"
            aria-label="Back to plugins"
            title="Back to plugins"
            on:click={() => (page = "plugins")}
          >
            <ArrowLeftIcon size={18} strokeWidth={2.4} />
          </button>
          <span>{selected.name}</span>
        </nav>
      {/if}

      <section class="settings-app-hero">
        <span class="app-icon"
          ><span>{selected.plugin.icon ?? selected.name.slice(0, 2)}</span
          ></span
        >
        <div>
          <h2>{selected.name}</h2>
          <p>{selected.plugin.description ?? selected.id}</p>
          <small
            >{selected.version} · {selected.enabled
              ? "enabled"
              : "disabled"}{selected.lastError
              ? ` · ${selected.lastError}`
              : ""}</small
          >
        </div>
      </section>

      {#if error || message}
        <section class="settings-group">
          <div class="settings-row static">
            <span>
              <span>{error ? "Plugin error" : "Plugin status"}</span>
              <small>{error || message}</small>
            </span>
          </div>
        </section>
      {/if}

      <section class="settings-group">
        <label class="settings-row permission-row">
          <span>
            <span>Enabled</span>
            <small>Load this plugin in the controller runtime.</small>
          </span>
          <input
            type="checkbox"
            checked={selected.enabled}
            disabled={Boolean(busy)}
            on:change={(event) =>
              togglePlugin(selected, event.currentTarget.checked)}
          />
        </label>
        {#if selected.installed}
          <div class="settings-row static settings-actions-row">
            <span>
              <span>Installed plugin</span>
              <small
                >Uninstalling disables the runtime but keeps isolated storage
                and logs.</small
              >
            </span>
            <button
              class="settings-inline-button danger"
              disabled={Boolean(busy)}
              on:click={() => uninstall(selected.id)}
            >
              <TrashIcon size={14} strokeWidth={2.4} />
              Uninstall
            </button>
          </div>
        {/if}
      </section>

      {#if selectedAdministratorPermissions.length > 0}
        <section class="settings-group">
          <label class="settings-row permission-row">
            <span>
              <span>Administrator access</span>
              <small
                >Required for risky capabilities: {selectedAdministratorPermissions.join(
                  ", ",
                )}.</small
              >
            </span>
            <input
              type="checkbox"
              checked={selected.administratorTrusted}
              disabled={Boolean(busy)}
              on:change={(event) =>
                toggleAdministrator(event.currentTarget.checked)}
            />
          </label>
        </section>
      {/if}

      <section class="settings-group">
        {#if selectedPermissions.length === 0}
          <div class="settings-row static">
            <span>
              <span>No permissions</span>
              <small>This plugin declares no capabilities.</small>
            </span>
          </div>
        {:else}
          <div class="settings-row static">
            <PermissionCards
              permissions={selectedPermissions}
              granted={selectedPermissions.filter(granted)}
              describe={describePluginPermission}
              disabled={Boolean(busy)}
              onToggle={togglePermission}
            />
          </div>
        {/if}
      </section>

      {#if selectedSettings.length > 0}
        <section class="settings-group">
          {#each selectedSettings as [key, setting]}
            <div class="settings-row static settings-actions-row">
              <span>
                <span>{setting.label || key}</span>
                <small>{setting.description || key}</small>
              </span>
              {#if setting.type === "bool"}
                <input
                  type="checkbox"
                  checked={Boolean(settingsDraft[key])}
                  on:change={(event) =>
                    setSetting(key, event.currentTarget.checked)}
                />
              {:else if setting.type === "number"}
                <input
                  class="settings-text-input"
                  type="number"
                  value={Number(settingsDraft[key] ?? setting.default ?? 0)}
                  on:input={(event) =>
                    setSetting(key, Number(event.currentTarget.value))}
                />
              {:else if setting.type === "select"}
                <select
                  class="settings-text-input"
                  value={String(settingsDraft[key] ?? setting.default ?? "")}
                  on:change={(event) =>
                    setSetting(key, event.currentTarget.value)}
                >
                  {#each setting.options ?? [] as option}
                    <option value={option}>{option}</option>
                  {/each}
                </select>
              {:else if setting.type === "secret"}
                <input
                  class="settings-text-input"
                  type="password"
                  placeholder={settingsDraft[key] ? "Stored secret" : ""}
                  value={secretDraft[key] ?? ""}
                  on:input={(event) =>
                    setSecret(key, event.currentTarget.value)}
                />
              {:else}
                <input
                  class="settings-text-input"
                  value={String(settingsDraft[key] ?? setting.default ?? "")}
                  on:input={(event) =>
                    setSetting(key, event.currentTarget.value)}
                />
              {/if}
            </div>
          {/each}
          <div class="settings-row static settings-actions-row">
            <span>
              <span>Settings</span>
              <small>Saved values are available through ctx.settings.</small>
            </span>
            <button
              class="settings-inline-button"
              disabled={Boolean(busy)}
              on:click={saveSettings}>Save</button
            >
          </div>
        </section>
      {/if}

      <section class="settings-group">
        <div class="settings-row static">
          <span>
            <span>Plugin storage</span>
            <small>
              {storageKeys.length === 1
                ? "1 isolated record"
                : `${storageKeys.length} isolated records`}
            </small>
          </span>
          {#if storageKeys.length > 0}
            <button
              class="settings-inline-button danger"
              disabled={Boolean(busy)}
              type="button"
              on:click={clearSelectedPluginStorage}
            >
              Clear
            </button>
          {/if}
        </div>
      </section>

      {#if selected.commands?.length}
        <section class="settings-group">
          {#each selected.commands as command}
            <div class="settings-row static settings-actions-row">
              <span>
                <span>{command.title}</span>
                <small>{command.subtitle || command.id}</small>
              </span>
              <button
                class="settings-inline-button"
                disabled={Boolean(busy)}
                on:click={() => runCommand(command.id)}
              >
                <PlayIcon size={14} strokeWidth={2.4} />
                Run
              </button>
            </div>
          {/each}
          {#if runOutput}
            <div class="settings-row static">
              <span>
                <span>Command result</span>
                <small>{runOutput}</small>
              </span>
            </div>
          {/if}
        </section>
      {/if}

      <section class="settings-group plugin-log-section">
        {#if logs.length === 0}
          <div class="settings-row static">
            <span>
              <span>No audit entries</span>
              <small
                >Runtime actions and permission denials will appear here.</small
              >
            </span>
          </div>
        {:else}
          {#each logs.slice(0, 12) as item}
            <div class:failed={!item.ok} class="plugin-log-card">
              <span class="plugin-log-icon">
                {#if item.ok}
                  <CheckCircleIcon size={17} strokeWidth={2.4} />
                {:else}
                  <XCircleIcon size={17} strokeWidth={2.4} />
                {/if}
              </span>
              <span class="plugin-log-copy">
                <span>{item.action}</span>
                <small
                  >{new Date(item.at).toLocaleString()}{item.permission
                    ? ` · ${item.permission}`
                    : ""}</small
                >
                {#if item.error || item.detail}
                  <small>{item.error || item.detail}</small>
                {/if}
              </span>
            </div>
          {/each}
        {/if}
      </section>
    </div>
  </section>
{:else}
  <section
    class:settings-stack={!embedded}
    class:plugin-manager-shell={embedded}
  >
    <div class:settings-page={!embedded} class:plugin-manager-page={embedded}>
      {#if error || message}
        <section class="settings-group">
          <div class="settings-row static">
            <span>
              <span>{error ? "Plugin error" : "Plugin status"}</span>
              <small>{error || message}</small>
            </span>
          </div>
        </section>
      {/if}

      {#if installed.length === 0}
        <section class="empty-with-action">
          <EmptyState
            title="No plugins installed"
            body="Add signed plugin sources or create local plugins under /plugins."
          />
        </section>
      {:else}
        <section class="settings-group">
          {#each installed as plugin}
            <button
              class="settings-row app-row"
              on:click={() => openPlugin(plugin)}
            >
              <span class="app-icon"
                ><span>{plugin.plugin.icon ?? plugin.name.slice(0, 2)}</span
                ></span
              >
              <span>
                <span>{plugin.name}</span>
                <small
                  >{pluginSubtitle(plugin)} · {plugin.enabled
                    ? "on"
                    : "off"}</small
                >
              </span>
              <ChevronRightIcon size={18} strokeWidth={2.4} />
            </button>
          {/each}
        </section>
      {/if}
    </div>
  </section>
{/if}

{#if pendingPluginInstall}
  <div class="permission-scrim" role="presentation">
    <section
      class="permission-dialog permission-dialog-wide"
      role="dialog"
      aria-modal="true"
    >
      <div>
        <h2>Install {pendingPluginInstall.plugin.name}</h2>
        <p>Choose what starts enabled.</p>
      </div>
      <PermissionCards
        permissions={pendingPluginInstall.plugin.permissions ?? []}
        granted={pendingPluginPermissions}
        describe={describePluginPermission}
        onToggle={togglePendingPluginPermission}
      />
      <div class="permission-actions">
        <button
          class="permission-secondary"
          on:click={() => (pendingPluginInstall = null)}>Cancel</button
        >
        <button
          class="permission-primary"
          on:click={() => {
            const entry = pendingPluginInstall;
            pendingPluginInstall = null;
            if (entry) void install(entry);
          }}>Install</button
        >
      </div>
    </section>
  </div>
{/if}
