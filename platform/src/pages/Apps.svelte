<script lang="ts">
  import { createEventDispatcher, onDestroy, onMount } from "svelte";
  import ArrowLeftIcon from "lucide-svelte/icons/arrow-left";
  import MaximizeIcon from "lucide-svelte/icons/maximize";
  import MinimizeIcon from "lucide-svelte/icons/minimize";
  import RotateCwIcon from "lucide-svelte/icons/rotate-cw";
  import PlayIcon from "lucide-svelte/icons/play";
  import PlusIcon from "lucide-svelte/icons/plus";
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
  import { selectJumpBackIn, type JumpEntry } from "../jump-back-in";
  import { loadLibraryRecent, onLibraryChanged, type RecentLibraryTitle } from "../sdk/library";
  import { setLaunchItem } from "../sdk/apps/launch-intent";
  import { pad, type PadState } from "../sdk/pad";
  import type { YuiDevApp } from "../sdk/apps";
  import type { SidebarThemeSettings } from "../types";

  export let openAppRequest: { appId: string; nonce: number } | null = null;
  export let theme: SidebarThemeSettings = { enabled: true, images: [] };

  let recents: RecentApp[] = [];
  let recentLoad = 0;
  let libraryTitles: RecentLibraryTitle[] = [];
  let libraryLoad = 0;
  let padState: PadState = { session: null, players: [] };
  let filter = "";
  let now = Date.now();

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
      void refreshRecents();
      void refreshLibrary();
    };
    window.addEventListener("yui:apps-changed", onAppsChanged);
    window.addEventListener("yui:account-changed", onAccountChanged);

    void refreshApps();
    void refreshRecents();
    void refreshLibrary();
    const offLibrary = onLibraryChanged(() => void refreshLibrary());
    void pad.state().then((state) => (padState = state));
    const offPad = pad.onState((state) => (padState = state));
    const clock = window.setInterval(() => (now = Date.now()), 60000);

    return () => {
      offPad();
      offLibrary();
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
  // Jump back in: library titles first, recently opened apps as the fallback.
  $: jump = selectJumpBackIn({
    apps,
    titles: libraryTitles,
    recents,
    starter: apps.find(isGame) ?? apps[0] ?? null,
  });
  $: hero = jump.hero;
  $: resumeRow = jump.row;

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

  async function refreshLibrary() {
    const ticket = ++libraryLoad;
    const titles = await loadLibraryRecent();
    if (ticket === libraryLoad) libraryTitles = titles;
  }

  function entryDetail(entry: JumpEntry<YuiDevApp>, now: number) {
    if (entry.title) {
      return `${entry.app.name} · ${entry.at ? `played ${openedAgo(entry.at, now)}` : "not played yet"}`;
    }
    return `${categoryOf(entry.app)} · ${entry.at ? `opened ${openedAgo(entry.at, now)}` : "not opened yet"}`;
  }

  function openEntry(entry: JumpEntry<YuiDevApp>) {
    launch(
      entry.app,
      entry.title ? { id: entry.title.id, title: entry.title.title, resume: entry.title.resume ?? null } : null,
    );
  }

  async function refreshRecents() {
    const ticket = ++recentLoad;
    const items = await loadRecentApps();
    if (ticket === recentLoad) recents = items;
  }

  async function launch(app: YuiDevApp, item: { id: string; title: string; resume: unknown } | null = null) {
    setLaunchItem(app.id, item);
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
          {#if hero.art}
            <img class="jump-art" src={hero.art} alt="" />
          {:else if isGame(hero.app)}
            <Cartridge app={hero.app} size="hero" />
          {:else}
            <span class="jump-icon"><AppIcon app={hero.app} size={84} /></span>
          {/if}
        </span>
        <div class="jump-copy">
          <div class="jump-heading">
            <span class="jump-eyebrow">{hero.title || hero.at ? "JUMP BACK IN" : "START HERE"}</span>
            <h1>{hero.name}</h1>
            <p>{entryDetail(hero, now)}</p>
            {#if hero.progress !== null}
              <div
                class="jump-progress"
                role="progressbar"
                aria-label="Progress"
                aria-valuemin="0"
                aria-valuemax="100"
                aria-valuenow={Math.round(hero.progress * 100)}
              >
                <span style={`width: ${hero.progress * 100}%`}></span>
              </div>
            {/if}
          </div>
          <div class="jump-actions">
            <button class="jump-resume" on:click={() => hero && openEntry(hero)}>
              <PlayIcon size={18} strokeWidth={0} fill="currentColor" />
              {hero.title || hero.at ? "Resume" : "Open"}
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
        {#each resumeRow as item (item.key)}
          <button class="resume-card" on:click={() => openEntry(item)}>
            <span class="resume-badge">
              {#if item.art}
                <img class="resume-art" src={item.art} alt="" />
              {:else}
                <AppIcon app={item.app} size={30} />
              {/if}
            </span>
            <span class="resume-text">
              <strong>{item.name}</strong>
              <span>{entryDetail(item, now)}</span>
            </span>
          </button>
        {/each}
      </section>
    {/if}

    <section class="all-apps" aria-label="All apps">
      <header class="all-apps-head">
        <h2>All apps</h2>
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
