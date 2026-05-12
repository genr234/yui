<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import OnScreenKeyboard from "./components/OnScreenKeyboard.svelte";
  import PermissionCards from "./components/PermissionCards.svelte";
  import Sidebar from "./components/Sidebar.svelte";
  import TitleBar from "./components/TitleBar.svelte";
  import Router from "./pages/Router.svelte";
  import { findRoute, routes } from "./pages/routes";
  import { bridge } from "./sdk/bridge";
  import type {
    ActionItem,
    BridgeState,
    DetailItem,
    Section,
    SectionItem,
  } from "./types";
  import ChromeIcon from "lucide-svelte/icons/chrome";
  import FolderIcon from "lucide-svelte/icons/folder";
  import RefreshCwIcon from "lucide-svelte/icons/refresh-cw";
  import { loadApps } from "./apps";
  import { resolveSubtitle } from "@/pages/subtitle";
  import {
    describePermission,
    type PermissionRequest,
  } from "@/sdk/apps/permissions";
  import { plugins } from "@/sdk/plugins";

  let open = false;
  let section: Section = "home";
  let status: any = null;
  let diagnostics = "";
  let config: any = null;
  let bridgeState: BridgeState = "connecting";
  let pressTimer: number | undefined;
  let appRouteActive = false;
  let appFullscreen = false;
  let permissionPrompt: PermissionRequest | null = null;
  let closing = false;
  let closeTimer: number | undefined;
  let appCount: number | null = null;
  let pluginCount: number | null = null;
  let pluginSettingsRequest: { pluginId: string; nonce: number } | null = null;
  let pluginSettingsNonce = 0;

  const sections: SectionItem[] = routes;

  onMount(() => {
    void refresh();
    const offFullscreen = (event: Event) => {
      appFullscreen = Boolean(
        (event as CustomEvent<{ active: boolean }>).detail?.active,
      );
    };
    const offPermissionRequest = (event: Event) => {
      permissionPrompt = (event as CustomEvent<PermissionRequest>).detail;
    };
    window.addEventListener("yui:shell-fullscreen", offFullscreen);
    window.addEventListener("yui:permission-request", offPermissionRequest);
    return () => {
      window.removeEventListener("yui:shell-fullscreen", offFullscreen);
      window.removeEventListener(
        "yui:permission-request",
        offPermissionRequest,
      );
    };
  });

  onDestroy(() => {
    appFullscreen = false;
  });

  async function refresh() {
    try {
      const [statusResult, diagnosticsResult, configResult] = await Promise.all(
        [
          bridge.send<any>("status.get"),
          bridge.send<{ text: string }>("diagnostics.get"),
          bridge.send<any>("config.get"),
        ],
      );
      status = statusResult;
      diagnostics = diagnosticsResult?.text ?? "";
      config = configResult;
      bridgeState = "online";
      void refreshSubtitles();
    } catch (error) {
      bridgeState = "offline";
      diagnostics = error instanceof Error ? error.message : String(error);
    }
  }

  async function refreshSubtitles() {
    const [appsResult, pluginsResult] = await Promise.all([
      loadApps().then((items) => items.length).catch(() => null),
      plugins.list().then((items) => items.length).catch(() => null),
    ]);
    appCount = appsResult;
    pluginCount = pluginsResult;
  }

  function startPress() {
    clearPress();
    pressTimer = window.setTimeout(() => {
      openMenu();
      void refresh();
    }, 850);
  }

  function clearPress() {
    if (pressTimer) {
      window.clearTimeout(pressTimer);
      pressTimer = undefined;
    }
  }

  function alwaysOpen() {
    return localStorage.getItem("yui_always_open") === "true";
  }

  function openMenu() {
    window.clearTimeout(closeTimer);
    closing = false;
    open = true;
    void refreshSubtitles();
  }

  function closeMenu() {
    if (alwaysOpen()) return;
    open = false;
    closing = true;
    closeTimer = window.setTimeout(() => {
      closing = false;
    }, 240);
  }

  async function reimportConfig() {
    await bridge.send("platform.reimport");
    await refresh();
  }

  async function selectChrome() {
    await bridge.send("platform.selectChrome");
    await refresh();
  }

  function metric(value: unknown, fallback = "Unknown") {
    return value === undefined || value === null || value === ""
      ? fallback
      : String(value);
  }

  function answerPermission(granted: boolean) {
    permissionPrompt?.resolve(granted);
    permissionPrompt = null;
  }

  function answerAllPermissions(granted: boolean) {
    permissionPrompt?.resolveAll?.(granted);
    permissionPrompt = null;
  }

  function openPluginSettings(pluginId: string) {
    pluginSettingsNonce += 1;
    pluginSettingsRequest = { pluginId, nonce: pluginSettingsNonce };
    section = "settings";
  }

  $: activeRoute = findRoute(section);
  $: activeSubtitle = resolveSubtitle(activeRoute, { appCount, pluginCount });
  $: shellRendered = open || closing || alwaysOpen();
  $: shellClosing = closing && !open && !alwaysOpen();
  $: showTitleBar = !(section === "apps" && appRouteActive);
  $: homeActions = [
    {
      label: "Refresh Status",
      icon: RefreshCwIcon,
      tone: "blue",
      run: refresh,
    },
    {
      label: "Re-import Config",
      icon: FolderIcon,
      tone: "green",
      run: reimportConfig,
    },
    {
      label: "Select Chrome",
      icon: ChromeIcon,
      tone: "violet",
      run: selectChrome,
    },
  ] satisfies ActionItem[];
  $: toolActions = [
    {
      label: "Refresh Diagnostics",
      icon: RefreshCwIcon,
      tone: "blue",
      run: refresh,
    },
    {
      label: "Re-import Kiosk Batch",
      icon: FolderIcon,
      tone: "green",
      run: reimportConfig,
    },
    {
      label: "Select Chrome",
      icon: ChromeIcon,
      tone: "orange",
      run: selectChrome,
    },
  ] satisfies ActionItem[];
  $: settingDetails = [
    { label: "HTTP", value: metric(config?.platform_http_addr) },
    { label: "Bridge", value: metric(config?.platform_bridge_addr) },
    {
      label: "Debug port",
      value: metric(config?.platform_remote_debugging_port),
    },
    { label: "Chrome", value: metric(config?.chrome_path) },
    {
      label: "Config",
      value: metric(
        config?.config_path ?? config?.ConfigPath,
        "Active config loaded",
      ),
    },
    { label: "Status", value: metric(config?.status_path) },
    { label: "User data", value: metric(config?.user_data_dir) },
  ] satisfies DetailItem[];
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
  on:keydown={(event) => event.key === "Enter" && openMenu()}
></div>

{#if shellRendered}
  <button
    class:closing={shellClosing}
    class="veil"
    aria-label="Close Yui menu"
    on:click={closeMenu}
  ></button>

  <section
    class:app-route-active={section === "apps" && appRouteActive}
    class:app-fullscreen={appFullscreen}
    class:closing={shellClosing}
    class="shell"
    aria-label="Yui Platform"
  >
    <Sidebar
      {sections}
      active={section}
      version={status?.version ?? "0.1.0"}
      on:select={(event) => (section = event.detail)}
    />

    <div class="workspace">
      <main class="main">
        {#if showTitleBar}
          <TitleBar
            title={activeRoute.label}
            subtitle={activeSubtitle}
            on:refresh={refresh}
          />
        {/if}
        <Router
          {section}
          {homeActions}
          {toolActions}
          {settingDetails}
          {config}
          {diagnostics}
          {pluginSettingsRequest}
          on:appLaunched={(event) => (appRouteActive = event.detail.active)}
          on:pluginSettings={(event) =>
            openPluginSettings(event.detail.pluginId)}
          on:pluginSettingsBack={() => (section = "plugins")}
        />
      </main>
    </div>
  </section>

  {#if permissionPrompt}
    <div class="permission-scrim" role="presentation">
      <section
        class="permission-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="permission-title"
      >
        <div>
          <h2 id="permission-title">{permissionPrompt.app.name} wants access</h2>
          <p>You can change this later in Settings.</p>
        </div>
        <PermissionCards
          permissions={permissionPrompt.permissions ?? [permissionPrompt.permission]}
          granted={[permissionPrompt.permission]}
          describe={describePermission}
          readonly
        />
        <div class="permission-actions">
          <button
            class="permission-secondary"
            on:click={() => answerAllPermissions(false)}>Deny all</button
          >
          <button
            class="permission-secondary"
            on:click={() => answerPermission(false)}>Deny</button
          >
          <button
            class="permission-primary"
            on:click={() => answerPermission(true)}>Grant</button
          >
          <button
            class="permission-primary"
            on:click={() => answerAllPermissions(true)}>Allow all</button
          >
        </div>
      </section>
    </div>
  {/if}

  <OnScreenKeyboard />
{/if}
