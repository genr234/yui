<script lang="ts">
  import { createEventDispatcher, onDestroy, onMount } from "svelte";
  import ArrowLeftIcon from "lucide-svelte/icons/arrow-left";
  import MaximizeIcon from "lucide-svelte/icons/maximize";
  import MinimizeIcon from "lucide-svelte/icons/minimize";
  import RotateCwIcon from "lucide-svelte/icons/rotate-cw";
  import EmptyState from "../components/EmptyState.svelte";
  import AppHost from "../components/apps/AppHost.svelte";
  import { discoverDevApps, type YuiDevApp } from "../sdk/apps";

  let apps: YuiDevApp[] = [];
  let selectedId = "";
  let launchedId = "";
  let reloadKey = 0;
  let error = "";
  let loading = true;
  let shellFullscreen = false;
  const dispatch = createEventDispatcher<{ launched: { active: boolean } }>();

  onMount(() => {
    const offFullscreen = (event: Event) => {
      shellFullscreen = Boolean(
        (event as CustomEvent<{ active: boolean }>).detail?.active,
      );
    };
    window.addEventListener("yui:shell-fullscreen", offFullscreen);

    void (async () => {
      try {
        apps = await discoverDevApps();
        selectedId = apps[0]?.id ?? "";
      } catch (err) {
        error = err instanceof Error ? err.message : String(err);
      } finally {
        loading = false;
      }
    })();

    return () =>
      window.removeEventListener("yui:shell-fullscreen", offFullscreen);
  });

  onDestroy(() => {
    announceLaunched(false);
    setShellFullscreen(false);
  });

  $: selected = apps.find((app) => app.id === selectedId) ?? apps[0];
  $: launched = apps.find((app) => app.id === launchedId);
  $: categories = groupApps(apps);

  function groupApps(items: YuiDevApp[]) {
    const groups = new Map<string, YuiDevApp[]>();
    for (const app of items) {
      const category = normalizeCategory(app.app.category);
      groups.set(category, [...(groups.get(category) ?? []), app]);
    }
    return [...groups.entries()].map(([name, items]) => ({ name, items }));
  }

  function normalizeCategory(category?: string) {
    const value = category?.trim();
    if (!value) return "Utilities";
    return value.charAt(0).toUpperCase() + value.slice(1);
  }

  function isImageIcon(icon?: string) {
    return Boolean(
      icon &&
        (/^(https?:|data:|\/|\.)/.test(icon) ||
          /\.(png|jpe?g|gif|webp|svg)$/i.test(icon)),
    );
  }

  function isGameCategory(category: string) {
    return category.toLowerCase() === "games";
  }

  function launch(app: YuiDevApp) {
    selectedId = app.id;
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
    if (!launched?.app.permissions?.includes("fullscreen")) return;
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
</script>

{#if loading}
  <EmptyState title="Loading apps" body="Scanning local yui app manifests." />
{:else if error}
  <EmptyState title="App discovery failed" body={error} />
{:else if apps.length === 0}
  <EmptyState
    title="No simple apps found"
    body="Add apps under /apps with a yui.app.json manifest and app.yui.js entry."
  />
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
      {#if launched.app.permissions?.includes("fullscreen")}
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
              class:game={isGameCategory(category.name)}
              class="app-tile"
              on:click={() => launch(app)}
            >
              <span class="app-tile-icon" aria-hidden="true">
                {#if isGameCategory(category.name)}
                  <span class="cartridge">
                    <span class="cartridge-brand">YUI</span>
                    <span class="cartridge-ridges left"></span>
                    <span class="cartridge-ridges right"></span>
                    <span class="cartridge-label">
                      {#if isImageIcon(app.app.icon)}
                        <img src={app.app.icon} alt="" />
                      {:else}
                        <span>{app.app.icon ?? app.name.slice(0, 4)}</span>
                      {/if}
                    </span>
                    <span class="cartridge-notch"></span>
                  </span>
                {:else if isImageIcon(app.app.icon)}
                  <img src={app.app.icon} alt="" width="64" height="64" />
                {:else}
                  <span>{app.app.icon ?? "◇"}</span>
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
