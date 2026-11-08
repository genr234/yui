<script lang="ts">
  import { createEventDispatcher, onDestroy, onMount } from "svelte";
  import ArrowLeftIcon from "lucide-svelte/icons/arrow-left";
  import MaximizeIcon from "lucide-svelte/icons/maximize";
  import MinimizeIcon from "lucide-svelte/icons/minimize";
  import RotateCwIcon from "lucide-svelte/icons/rotate-cw";
  import PlayIcon from "lucide-svelte/icons/play";
  import PlusIcon from "lucide-svelte/icons/plus";
  import SmartphoneIcon from "lucide-svelte/icons/smartphone";
  import EmptyState from "../components/EmptyState.svelte";
  import JoinQr from "../components/JoinQr.svelte";
  import ThemeParticles from "../components/ThemeParticles.svelte";
  import AppHost from "../components/apps/AppHost.svelte";
  import AppIcon from "../components/apps/AppIcon.svelte";
  import Cartridge from "../components/apps/Cartridge.svelte";
  import {
    appHasPermission,
    findApp,
    groupAppsByCategory,
    isGameAppCategory,
    loadApps,
    normalizeAppCategory,
  } from "../apps";
  import { openedAgo, loadRecentApps, recordRecentApp, type RecentApp } from "../recent-apps";
  import { pad, type PadState } from "../sdk/pad";
  import type { YuiDevApp } from "../sdk/apps";
  import type { SidebarThemeSettings } from "../types";

  export let openAppRequest: { appId: string; nonce: number } | null = null;
  export let theme: SidebarThemeSettings = { enabled: true, images: [] };

  let recents: RecentApp[] = [];
  let recentLoad = 0;
  let padState: PadState = { session: null, players: [] };
  let filter = "";
  let now = Date.now();

  let apps: YuiDevApp[] = [];
  let launchedId = "";
  let reloadKey = 0;
  let error = "";
  let loading = true;
  let shellFullscreen = false;
  let playBusy = false;
  let playError = "";
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
      void refreshRecents();
    };
    window.addEventListener("yui:apps-changed", onAppsChanged);
    window.addEventListener("yui:account-changed", onAccountChanged);

    void refreshApps();
    void refreshRecents();
    void pad.state().then((state) => (padState = state));
    const offPad = pad.onState((state) => (padState = state));
    const clock = window.setInterval(() => (now = Date.now()), 60000);

    return () => {
      offPad();
      window.clearInterval(clock);
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
  $: categoryNames = groupAppsByCategory(apps).map((category) => category.name);
  $: if (filter && !categoryNames.includes(filter)) filter = "";
  $: visibleApps = filter ? apps.filter((app) => categoryOf(app) === filter) : apps;
  $: recentOpened = recents
    .map((entry) => ({ app: findApp(apps, entry.id), openedAt: entry.openedAt }))
    .filter((entry): entry is { app: YuiDevApp; openedAt: number } => Boolean(entry.app));
  $: heroRecent = recentOpened[0] ?? null;
  $: hero = heroRecent?.app ?? apps.find(isGame) ?? apps[0] ?? null;
  $: resumeRow = [
    ...recentOpened.slice(1),
    ...apps
      .filter((app) => app.id !== hero?.id && !recentOpened.some((entry) => entry.app.id === app.id))
      .map((app) => ({ app, openedAt: 0 })),
  ].slice(0, 3);

  async function togglePhonePlay() {
    playBusy = true;
    playError = "";
    try {
      padState = padState.session ? await pad.end() : await pad.start();
    } catch (err) {
      playError = err instanceof Error ? err.message : "Could not change phone play";
    } finally {
      playBusy = false;
    }
  }

  function categoryOf(app: YuiDevApp) {
    return normalizeAppCategory(app.app.category);
  }

  function isGame(app: YuiDevApp) {
    return isGameAppCategory(categoryOf(app));
  }
  $: if (
    openAppRequest &&
    openAppRequest.nonce !== consumedOpenAppNonce &&
    apps.length > 0
  ) {
    consumedOpenAppNonce = openAppRequest.nonce;
    const requested = findApp(apps, openAppRequest.appId);
    if (requested) launch(requested);
  }

  async function refreshRecents() {
    const ticket = ++recentLoad;
    const items = await loadRecentApps();
    if (ticket === recentLoad) recents = items;
  }

  async function launch(app: YuiDevApp) {
    now = Date.now();
    launchedId = app.id;
    reloadKey += 1;
    announceLaunched(true);
    const ticket = ++recentLoad;
    try {
      const items = await recordRecentApp(app.id);
      if (ticket === recentLoad) recents = items;
    } catch {
      // The read failed, or the account store changed before the write.
      // Leave the list alone so this launch cannot publish another account's history.
    }
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
    {#if hero}
      <section class="jump-banner" aria-label="Jump back in">
        <ThemeParticles {theme} />
        <div class="jump-stripes" aria-hidden="true">
          <span></span><span></span><span></span>
        </div>
        <span class="jump-cartridge">
          {#if isGame(hero)}
            <Cartridge app={hero} size="hero" />
          {:else}
            <span class="jump-icon"><AppIcon app={hero} size={84} /></span>
          {/if}
        </span>
        <div class="jump-copy">
          <div class="jump-heading">
            <span class="jump-eyebrow">{heroRecent ? "JUMP BACK IN" : "START HERE"}</span>
            <h1>{hero.name}</h1>
            <p>
              {categoryOf(hero)}
              {#if heroRecent}· opened {openedAgo(heroRecent.openedAt, now)}{/if}
            </p>
          </div>
          <div class="jump-actions">
            <button class="jump-resume" on:click={() => hero && launch(hero)}>
              <PlayIcon size={18} strokeWidth={0} fill="currentColor" />
              {heroRecent ? "Resume" : "Open"}
            </button>
            {#if padState.session}
              <div class="join-chip">
                <span class="join-copy">
                  <span>Join on your phone</span>
                  <strong>{padState.session.code}</strong>
                </span>
                <JoinQr value={padState.session.join_url} label={`QR code to join session ${padState.session.code}`} />
              </div>
            {/if}
          </div>
        </div>
      </section>
    {/if}

    {#if resumeRow.length > 0}
      <section class="resume-row" aria-label="More from your apps">
        {#each resumeRow as item (item.app.id)}
          <button class="resume-card" on:click={() => launch(item.app)}>
            <span class="resume-badge"><AppIcon app={item.app} size={30} /></span>
            <span class="resume-text">
              <strong>{item.app.name}</strong>
              <span>
                {categoryOf(item.app)} · {item.openedAt ? `opened ${openedAgo(item.openedAt, now)}` : "not opened yet"}
              </span>
            </span>
          </button>
        {/each}
      </section>
    {/if}

    <section class="all-apps" aria-label="All apps">
      <header class="all-apps-head">
        <h2>All apps</h2>
        <button class="play-phones" type="button" disabled={playBusy} on:click={togglePhonePlay}>
          <SmartphoneIcon size={18} strokeWidth={2.2} />
          {padState.session ? "End phone play" : "Play on your phones"}
        </button>
        {#if playError}<span class="play-phones-error">{playError}</span>{/if}
        {#if categoryNames.length > 1}
          <div class="category-chips" role="group" aria-label="Filter by category">
            <button class:active={filter === ""} aria-pressed={filter === ""} on:click={() => (filter = "")}>All</button>
            {#each categoryNames as name}
              <button class:active={filter === name} aria-pressed={filter === name} on:click={() => (filter = name)}>{name}</button>
            {/each}
          </div>
        {/if}
        <span class="all-apps-count">{apps.length} installed</span>
      </header>
      <div class="app-shelf">
        {#each visibleApps as app (app.id)}
          <button class:game={isGame(app)} class="app-tile" on:click={() => launch(app)}>
            {#if isGame(app)}
              <Cartridge {app} />
            {:else}
              <span class="app-tile-icon" aria-hidden="true"><AppIcon {app} size={64} /></span>
            {/if}
            <span class="app-tile-name">{app.name}</span>
          </button>
        {/each}
        <button class="app-tile get-more" on:click={() => dispatch("store")}>
          <span class="app-tile-icon" aria-hidden="true"><PlusIcon size={32} strokeWidth={2.2} /></span>
          <span class="app-tile-name">Get more</span>
        </button>
      </div>
    </section>
  </section>
{/if}
