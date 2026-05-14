<script lang="ts">
  import { createEventDispatcher, onMount } from "svelte";
  import DownloadIcon from "lucide-svelte/icons/download";
  import PlusIcon from "lucide-svelte/icons/plus";
  import RefreshCwIcon from "lucide-svelte/icons/refresh-cw";
  import TrashIcon from "lucide-svelte/icons/trash-2";
  import XIcon from "lucide-svelte/icons/x";
  import {
    addAppSource,
    appCatalogEntryId,
    installedAppIds,
    installCatalogApp as installAppFromCatalog,
    loadAppsLibrary,
    refreshAppSource,
    removeAppSource,
    uninstallApp as uninstallInstalledApp,
  } from "../apps";
  import PermissionCards from "./PermissionCards.svelte";
  import {
    describePermission,
    setAppPermission,
  } from "../sdk/apps/permissions";
  import type { YuiCatalogEntry, YuiDevApp } from "../sdk/apps";
  import {
    describePluginPermission,
    pluginCatalog,
    pluginCatalogEntryId,
    pluginSources,
    plugins,
    type YuiPlugin,
    type YuiPluginCatalogEntry,
  } from "../sdk/plugins";
  import Plus from "lucide-svelte/icons/plus";

  export let kind: "apps" | "plugins";

  const dispatch = createEventDispatcher<{ close: void; changed: void }>();

  let sourceURL = "";
  let busy = "";
  let error = "";
  let message = "";
  let apps: YuiDevApp[] = [];
  let appCatalog: YuiCatalogEntry[] = [];
  let appSources: Awaited<ReturnType<typeof loadAppsLibrary>>["sources"] = [];
  let installedPlugins: YuiPlugin[] = [];
  let pluginCatalogEntries: YuiPluginCatalogEntry[] = [];
  let pluginSourceEntries: Awaited<ReturnType<typeof pluginSources.list>> = [];
  let pendingAppInstall: YuiCatalogEntry | null = null;
  let pendingAppPermissions: string[] = [];
  let pendingPluginInstall: YuiPluginCatalogEntry | null = null;
  let pendingPluginPermissions: string[] = [];
  let showSources = false;

  onMount(() => {
    void loadStore();
  });

  $: title = kind === "apps" ? "Install apps" : "Install plugins";
  $: emptyCatalog =
    kind === "apps"
      ? "No downloadable apps yet."
      : "No downloadable plugins yet.";
  $: catalogHelp =
    sources.length === 0
      ? "Add a trusted source to browse remote listings."
      : "Refresh a source to update remote listings.";
  $: sourcePlaceholder =
    kind === "apps"
      ? "https://example.com/yui/catalog.json"
      : "https://example.com/yui/plugin-catalog.json";
  $: sources = kind === "apps" ? appSources : pluginSourceEntries;
  $: appInstalledIds = installedAppIds(apps);
  $: pluginInstalledIds = new Set(
    installedPlugins
      .filter((plugin) => plugin.installed)
      .map((plugin) => plugin.id),
  );

  async function loadStore() {
    error = "";
    try {
      if (kind === "apps") {
        const library = await loadAppsLibrary();
        apps = library.apps;
        appSources = library.sources;
        appCatalog = library.catalog;
      } else {
        [installedPlugins, pluginSourceEntries, pluginCatalogEntries] =
          await Promise.all([
            plugins.list(),
            pluginSources.list().catch(() => []),
            pluginCatalog.list().catch(() => []),
          ]);
      }
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  async function addSource() {
    const url = sourceURL.trim();
    if (!url) return;
    busy = "source:add";
    error = "";
    message = "";
    try {
      if (kind === "apps") await addAppSource(url);
      else {
        const source = await pluginSources.add(url);
        await pluginSources.refresh(source.id);
      }
      sourceURL = "";
      message = "Source added and refreshed.";
      await loadStore();
      dispatch("changed");
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      await loadStore();
    } finally {
      busy = "";
    }
  }

  async function refreshSource(id: string) {
    busy = `source:refresh:${id}`;
    error = "";
    message = "";
    try {
      if (kind === "apps") await refreshAppSource(id);
      else await pluginSources.refresh(id);
      message = "Source refreshed.";
      await loadStore();
      dispatch("changed");
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      await loadStore();
    } finally {
      busy = "";
    }
  }

  async function removeSource(id: string) {
    busy = `source:remove:${id}`;
    error = "";
    message = "";
    try {
      if (kind === "apps") await removeAppSource(id);
      else await pluginSources.remove(id);
      message = "Source removed.";
      await loadStore();
      dispatch("changed");
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      await loadStore();
    } finally {
      busy = "";
    }
  }

  function startAppInstall(entry: YuiCatalogEntry) {
    pendingAppInstall = entry;
    pendingAppPermissions = [...(entry.app.permissions ?? [])];
  }

  async function installApp(entry: YuiCatalogEntry) {
    busy = `install:${appCatalogEntryId(entry)}`;
    error = "";
    message = "";
    try {
      await installAppFromCatalog(entry);
      for (const permission of entry.app.permissions ?? []) {
        setAppPermission(
          entry.app.id,
          permission,
          pendingAppPermissions.includes(permission),
        );
      }
      message = `${entry.app.name} installed.`;
      await loadStore();
      dispatch("changed");
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = "";
    }
  }

  async function uninstallApp(id: string) {
    busy = `uninstall:${id}`;
    error = "";
    message = "";
    try {
      await uninstallInstalledApp(id);
      message = "App uninstalled.";
      await loadStore();
      dispatch("changed");
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

  async function installPlugin(entry: YuiPluginCatalogEntry) {
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
      await loadStore();
      dispatch("changed");
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = "";
    }
  }

  function togglePendingPluginPermission(permission: string, granted: boolean) {
    const next = new Set(pendingPluginPermissions);
    if (granted) next.add(permission);
    else next.delete(permission);
    pendingPluginPermissions = [...next];
  }
</script>

<div class="store-scrim" role="presentation">
  <section
    class="store-dialog"
    role="dialog"
    aria-modal="true"
    aria-label={title}
  >
    <header class="store-header">
      <div>
        <h2>{title}</h2>
      </div>
      <div class="store-header-actions">
        <button
          class:active={showSources}
          class="icon-button title-action-button"
          type="button"
          title={showSources ? "Hide sources" : "Show sources"}
          aria-label={showSources ? "Hide sources" : "Show sources"}
          on:click={() => (showSources = !showSources)}
        >
          <PlusIcon size={18} strokeWidth={2.4} />
        </button>
        <button
          class="icon-button"
          type="button"
          aria-label="Close store"
          title="Close store"
          on:click={() => dispatch("close")}
        >
          <XIcon size={18} strokeWidth={2.4} />
        </button>
      </div>
    </header>

    {#if showSources}
      <section class="store-source-drawer">
        <div class="store-source-bar">
          <input
            class="settings-text-input"
            placeholder={sourcePlaceholder}
            bind:value={sourceURL}
            disabled={Boolean(busy)}
          />
          <button
            class="settings-inline-button"
            type="button"
            disabled={Boolean(busy) || !sourceURL.trim()}
            title="Add source"
            on:click={addSource}
          >
            <PlusIcon size={14} strokeWidth={2.4} />
            Add
          </button>
        </div>

        {#if sources.length > 0}
          <div class="store-source-list">
            {#each sources as source}
              <div class="store-source-row">
                <span>
                  <span>{source.name || source.url}</span>
                  <small>
                    {source.lastStatus}{source.publisher
                      ? ` · ${source.publisher}`
                      : ""}{source.lastError ? ` · ${source.lastError}` : ""}
                  </small>
                </span>
                <span class="settings-actions">
                  <button
                    class="icon-button compact"
                    type="button"
                    disabled={Boolean(busy)}
                    aria-label="Refresh source"
                    title="Refresh source"
                    on:click={() => refreshSource(source.id)}
                  >
                    <RefreshCwIcon size={15} strokeWidth={2.4} />
                  </button>
                  <button
                    class="icon-button compact danger"
                    type="button"
                    disabled={Boolean(busy)}
                    aria-label="Remove source"
                    title="Remove source"
                    on:click={() => removeSource(source.id)}
                  >
                    <TrashIcon size={15} strokeWidth={2.4} />
                  </button>
                </span>
              </div>
            {/each}
          </div>
        {/if}
      </section>
    {/if}

    {#if error || message}
      <div class="store-status">
        <span>{error ? "Store error" : "Store status"}</span>
        <small>{error || message}</small>
      </div>
    {/if}

    <section class="store-catalog" aria-label="Catalog">
      {#if kind === "apps"}
        {#if appCatalog.length === 0}
          <div class="store-empty">
            <h3>{emptyCatalog}</h3>
            <p>{catalogHelp}</p>
          </div>
        {:else}
          <div class="store-list">
            {#each appCatalog as entry}
              <div class="store-card">
                <span>
                  <span>{entry.app.name}</span>
                  <small>
                    {entry.app.version} · {entry.catalog}
                  </small>
                  {#if entry.app.description}
                    <small>{entry.app.description}</small>
                  {/if}
                </span>
                {#if appInstalledIds.has(entry.app.id)}
                  <button
                    class="settings-inline-button danger"
                    type="button"
                    disabled={Boolean(busy)}
                    on:click={() => uninstallApp(entry.app.id)}
                  >
                    <TrashIcon size={14} strokeWidth={2.4} />
                    Uninstall
                  </button>
                {:else}
                  <button
                    class="settings-inline-button"
                    type="button"
                    disabled={Boolean(busy)}
                    on:click={() => startAppInstall(entry)}
                  >
                    <DownloadIcon size={14} strokeWidth={2.4} />
                    Install
                  </button>
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      {:else if pluginCatalogEntries.length === 0}
        <div class="store-empty">
          <h3>{emptyCatalog}</h3>
          <p>{catalogHelp}</p>
        </div>
      {:else}
        <div class="store-list">
          {#each pluginCatalogEntries as entry}
            <div class="store-card">
              <span>
                <span>{entry.plugin.name}</span>
                <small>
                  {entry.plugin.version} · {entry.catalog}
                </small>
                {#if entry.plugin.description}
                  <small>{entry.plugin.description}</small>
                {/if}
              </span>
              {#if pluginInstalledIds.has(entry.plugin.id)}
                <span class="settings-inline-button" aria-disabled="true"
                  >Installed</span
                >
              {:else}
                <button
                  class="settings-inline-button"
                  type="button"
                  disabled={Boolean(busy)}
                  on:click={() => startPluginInstall(entry)}
                >
                  <DownloadIcon size={14} strokeWidth={2.4} />
                  Install
                </button>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </section>
  </section>
</div>

{#if pendingAppInstall}
  <div class="permission-scrim" role="presentation">
    <section
      class="permission-dialog permission-dialog-wide"
      role="dialog"
      aria-modal="true"
    >
      <div>
        <h2>Install {pendingAppInstall.app.name}</h2>
        <p>Choose what starts enabled.</p>
      </div>
      <PermissionCards
        permissions={pendingAppInstall.app.permissions ?? []}
        granted={pendingAppPermissions}
        describe={describePermission}
        onToggle={(permission, granted) => {
          const next = new Set(pendingAppPermissions);
          if (granted) next.add(permission);
          else next.delete(permission);
          pendingAppPermissions = [...next];
        }}
      />
      <div class="permission-actions">
        <button
          class="permission-secondary"
          on:click={() => (pendingAppInstall = null)}>Cancel</button
        >
        <button
          class="permission-primary"
          on:click={() => {
            const entry = pendingAppInstall;
            pendingAppInstall = null;
            if (entry) void installApp(entry);
          }}>Install</button
        >
      </div>
    </section>
  </div>
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
            if (entry) void installPlugin(entry);
          }}>Install</button
        >
      </div>
    </section>
  </div>
{/if}
