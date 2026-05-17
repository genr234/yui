<script lang="ts">
  import { createEventDispatcher, onDestroy, onMount } from "svelte";
  import ArrowLeftIcon from "lucide-svelte/icons/arrow-left";
  import MaximizeIcon from "lucide-svelte/icons/maximize";
  import MinimizeIcon from "lucide-svelte/icons/minimize";
  import RotateCwIcon from "lucide-svelte/icons/rotate-cw";
  import EmptyState from "../components/EmptyState.svelte";
  import AppHost from "../components/apps/AppHost.svelte";
  import {
    appHasPermission,
    fallbackAppIcon,
    findApp,
    groupAppsByCategory,
    isGameAppCategory,
    isImageAppIcon,
    loadApps,
  } from "../apps";
  import type { YuiDevApp } from "../sdk/apps";

  export let openAppRequest: { appId: string; nonce: number } | null = null;

  let apps: YuiDevApp[] = [];
  let launchedId = "";
  let reloadKey = 0;
  let error = "";
  let loading = true;
  let shellFullscreen = false;
  let consumedOpenAppNonce = 0;
  let refreshTimer: number | undefined;
  const dispatch = createEventDispatcher<{
    launched: { active: boolean };
    store: void;
  }>();

  onMount(() => {
    const offFullscreen = (event: Event) => {
      shellFullscreen = Boolean(
        (event as CustomEvent<{ active: boolean }>).detail?.active,
      );
    };
    window.addEventListener("yui:shell-fullscreen", offFullscreen);
    const onAppsChanged = () => {
      window.clearTimeout(refreshTimer);
      refreshTimer = window.setTimeout(() => {
        void refreshApps();
      }, 40);
    };
    const onAccountChanged = () => {
      closeApp();
      void refreshApps();
    };
    window.addEventListener("yui:apps-changed", onAppsChanged);
    window.addEventListener("yui:account-changed", onAccountChanged);

    void refreshApps();

    return () => {
      window.clearTimeout(refreshTimer);
      window.removeEventListener("yui:shell-fullscreen", offFullscreen);
      window.removeEventListener("yui:apps-changed", onAppsChanged);
      window.removeEventListener("yui:account-changed", onAccountChanged);
    };
  });

  onDestroy(() => {
    window.clearTimeout(refreshTimer);
    announceLaunched(false);
    setShellFullscreen(false);
  });

  $: launched = findApp(apps, launchedId);
  $: categories = groupAppsByCategory(apps);
  $: if (
    openAppRequest &&
    openAppRequest.nonce !== consumedOpenAppNonce &&
    apps.length > 0
  ) {
    consumedOpenAppNonce = openAppRequest.nonce;
    const requested = findApp(apps, openAppRequest.appId);
    if (requested) launch(requested);
  }

  function launch(app: YuiDevApp) {
    launchedId = app.id;
    reloadKey += 1;
    announceLaunched(true);
  }

  function closeApp() {
    setShellFullscreen(false);
    launchedId = "";
    announceLaunched(false);
  }

  function toggleFullscreen() {
    if (!appHasPermission(launched, "fullscreen")) return;
    setShellFullscreen(!shellFullscreen);
  }

  function setShellFullscreen(active: boolean) {
    window.dispatchEvent(
      new CustomEvent("yui:shell-fullscreen", {
        detail: { active, appId: launched?.id },
      }),
    );
  }

  function announceLaunched(active: boolean) {
    dispatch("launched", { active });
  }

  async function refreshApps() {
    loading = true;
    try {
      apps = await loadApps();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }
</script>

{#if loading}
  <EmptyState title="Loading apps" body="Scanning local yui app manifests." />
{:else if error}
  <EmptyState title="App discovery failed" body={error} />
{:else if apps.length === 0}
  <section class="empty-with-action">
    <EmptyState
      title="No apps found"
      body="Add apps from trusted catalogs or create local apps under /apps."
    />
  </section>
{:else if launched}
  <section class="app-route-view">
    <nav class="app-route-controls" aria-label="App navigation">
      <button
        class="icon-button"
        aria-label="Back to apps"
        title="Back to apps"
        on:click={closeApp}
      >
        <ArrowLeftIcon size={18} strokeWidth={2.4} />
      </button>
      <div class="app-route-title">
        <span>{launched.name}</span>
        <span>{launched.id} · {launched.version}</span>
      </div>
      <button
        class="icon-button"
        aria-label="Reload app"
        title="Reload app"
        on:click={() => (reloadKey += 1)}
      >
        <RotateCwIcon size={18} strokeWidth={2.4} />
      </button>
      {#if appHasPermission(launched, "fullscreen")}
        <button
          class="icon-button"
          aria-label={shellFullscreen ? "Exit fullscreen" : "Enter fullscreen"}
          title={shellFullscreen ? "Exit fullscreen" : "Enter fullscreen"}
          on:click={toggleFullscreen}
        >
          {#if shellFullscreen}
            <MinimizeIcon size={18} strokeWidth={2.4} />
          {:else}
            <MaximizeIcon size={18} strokeWidth={2.4} />
          {/if}
        </button>
      {/if}
    </nav>
    <AppHost devApp={launched} instanceKey={reloadKey} />
  </section>
{:else}
  <section class="apps-page">
    {#each categories as category}
      <section class="app-category-card" aria-label={category.name}>
        <h2>{category.name}</h2>
        <div class="app-shelf">
          {#each category.items as app}
            <button
              class:game={isGameAppCategory(category.name)}
              class="app-tile"
              on:click={() => launch(app)}
            >
              <span class="app-tile-icon" aria-hidden="true">
                {#if isGameAppCategory(category.name)}
                  <span class="cartridge">
                    <span class="cartridge-brand">YUI</span>
                    <span class="cartridge-ridges left"></span>
                    <span class="cartridge-ridges right"></span>
                    <span class="cartridge-label">
                      {#if isImageAppIcon(app.app.icon)}
                        <img src={app.app.icon} alt="" />
                      {:else}
                        <span>{fallbackAppIcon(app)}</span>
                      {/if}
                    </span>
                    <span class="cartridge-notch"></span>
                  </span>
                {:else if isImageAppIcon(app.app.icon)}
                  <img src={app.app.icon} alt="" width="64" height="64" />
                {:else}
                  <span>{fallbackAppIcon(app)}</span>
                {/if}
              </span>
              <span class="app-tile-name">{app.name}</span>
            </button>
          {/each}
        </div>
      </section>
    {/each}
  </section>
{/if}
