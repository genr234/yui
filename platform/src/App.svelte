<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import OnScreenKeyboard from "./components/OnScreenKeyboard.svelte";
  import FederatedStoreModal from "./components/FederatedStoreModal.svelte";
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
  import BlocksIcon from "lucide-svelte/icons/blocks";
  import CheckIcon from "lucide-svelte/icons/check";
  import EditIcon from "lucide-svelte/icons/pencil";
  import FolderIcon from "lucide-svelte/icons/folder";
  import LockIcon from "lucide-svelte/icons/lock";
  import DeleteIcon from "lucide-svelte/icons/delete";
  import PlusIcon from "lucide-svelte/icons/plus";
  import RefreshCwIcon from "lucide-svelte/icons/refresh-cw";
  import { loadApps } from "./apps";
  import { resolveSubtitle } from "@/pages/subtitle";
  import {
    describePermission,
    type PermissionRequest,
  } from "@/sdk/apps/permissions";
  import {
    plugins,
    type YuiPluginExtensions,
    type YuiPluginShellAction,
  } from "@/sdk/plugins";

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
  let appOpenRequest: { appId: string; nonce: number } | null = null;
  let appOpenNonce = 0;
  let homeEditing = false;
  let pluginSettingsRequest: { pluginId: string; nonce: number } | null = null;
  let pluginSettingsNonce = 0;
  let storeKind: "apps" | "plugins" | null = null;
  let authConfigured = false;
  let authUnlocked = false;
  let authVisible = false;
  let authMode: "setup" | "unlock" = "unlock";
  let authSetupStep: "create" | "confirm" = "create";
  let authPin = "";
  let authConfirmPin = "";
  let authMessage = "";
  let authBusy = false;
  let authRetryAfter = 0;
  let authPad = shuffleDigits();
  let authRetryTimer: number | undefined;
  let authEntry = "";
  let authEntryLength = 0;
  let authDotIndexes = [0, 1, 2, 3, 4, 5];
  let authSubmitLabel = "Enter";

  let extensions: YuiPluginExtensions = { pages: [], actions: [], css: [] };

  type AuthResult = {
    ok: boolean;
    token?: string;
    status: {
      configured: boolean;
      locked: boolean;
      retry_after_seconds: number;
    };
  };

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
    window.addEventListener("yui:apps-changed", refreshSubtitles);
    return () => {
      window.removeEventListener("yui:shell-fullscreen", offFullscreen);
      window.removeEventListener(
        "yui:permission-request",
        offPermissionRequest,
      );
      window.removeEventListener("yui:apps-changed", refreshSubtitles);
    };
  });

  onDestroy(() => {
    appFullscreen = false;
    if (authRetryTimer) window.clearInterval(authRetryTimer);
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
      await refreshAuthStatus();
      void refreshSubtitles();
      void refreshExtensions();
    } catch (error) {
      bridgeState = "offline";
      diagnostics = error instanceof Error ? error.message : String(error);
    }
  }

  async function refreshSubtitles() {
    const [appsResult, pluginsResult] = await Promise.all([
      loadApps()
        .then((items) => items.length)
        .catch(() => null),
      plugins
        .list()
        .then((items) => items.length)
        .catch(() => null),
    ]);
    appCount = appsResult;
    pluginCount = pluginsResult;
  }

  async function refreshExtensions() {
    const result = await plugins.extensions().catch(() => null);
    extensions = {
      pages: result?.pages ?? [],
      actions: result?.actions ?? [],
      css: result?.css ?? [],
    };
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
    void requestAdminEntry();
  }

  function closeMenu() {
    if (alwaysOpen()) return;
    open = false;
    authUnlocked = false;
    authVisible = false;
    bridge.clearAuthToken();
    resetAuthEntry();
    closing = true;
    closeTimer = window.setTimeout(() => {
      closing = false;
    }, 240);
  }

  async function refreshAuthStatus() {
    const result = await bridge
      .send<{
        configured: boolean;
        locked: boolean;
        retry_after_seconds: number;
      }>("auth.status")
      .catch(() => null);
    if (!result) return;
    authConfigured = result.configured;
    authRetryAfter = result.retry_after_seconds ?? 0;
    if (!authConfigured) {
      authUnlocked = false;
    }
  }

  async function requestAdminEntry() {
    await refreshAuthStatus();
    if (!authConfigured) {
      showAuth("setup", "Create an admin PIN to protect Yui.");
      return;
    }
    if (authUnlocked) {
      showShell();
      return;
    }
    showAuth("unlock", authRetryAfter > 0 ? lockoutMessage() : "");
  }

  function showShell() {
    authVisible = false;
    authUnlocked = true;
    open = true;
    void refreshSubtitles();
    void refreshExtensions();
  }

  function showAuth(mode: "setup" | "unlock", message = "") {
    open = false;
    authVisible = true;
    authMode = mode;
    authMessage = message;
    resetAuthEntry();
    authPad = shuffleDigits();
  }

  function resetAuthEntry() {
    authPin = "";
    authConfirmPin = "";
    authSetupStep = "create";
  }

  function shuffleDigits() {
    const digits = ["0", "1", "2", "3", "4", "5", "6", "7", "8", "9"];
    for (let index = digits.length - 1; index > 0; index -= 1) {
      const random = crypto.getRandomValues(new Uint32Array(1))[0] / 2 ** 32;
      const swap = Math.floor(random * (index + 1));
      [digits[index], digits[swap]] = [digits[swap], digits[index]];
    }
    return digits;
  }

  function activePin() {
    return authMode === "setup" && authSetupStep === "confirm"
      ? authConfirmPin
      : authPin;
  }

  function setActivePin(value: string) {
    if (authMode === "setup" && authSetupStep === "confirm") {
      authConfirmPin = value;
    } else {
      authPin = value;
    }
  }

  function enterDigit(digit: string) {
    if (authBusy || authRetryAfter > 0) return;
    const next = `${activePin()}${digit}`.slice(0, 6);
    setActivePin(next);
    authMessage = "";
    authPad = shuffleDigits();
  }

  function deleteDigit() {
    if (authBusy) return;
    setActivePin(activePin().slice(0, -1));
    authPad = shuffleDigits();
  }

  function clearAuthPin() {
    if (authBusy) return;
    if (authMode === "setup" && authSetupStep === "confirm") {
      authConfirmPin = "";
      authSetupStep = "create";
    } else {
      setActivePin("");
    }
    authPad = shuffleDigits();
  }

  function lockoutMessage() {
    return authRetryAfter > 0
      ? `Too many attempts. Try again in ${authRetryAfter}s.`
      : "";
  }

  async function submitAuth() {
    if (authBusy || authRetryAfter > 0) return;
    authBusy = true;
    authMessage = "";
    try {
      if (authMode === "setup") {
        if (authSetupStep === "create") {
          if (authPin.length < 6) {
            authMessage = "Use at least 6 digits.";
            return;
          }
          authSetupStep = "confirm";
          authMessage = "";
          authPad = shuffleDigits();
          return;
        }
        if (authConfirmPin.length < 6) {
          authMessage = "Use at least 6 digits.";
          return;
        }
        if (authPin !== authConfirmPin) {
          authMessage = "PINs do not match.";
          authConfirmPin = "";
          authPad = shuffleDigits();
          return;
        }
        const result = await bridge.send<AuthResult>("auth.setPin", {
          pin: authPin,
        });
        if (result.token) bridge.setAuthToken(result.token);
        authConfigured = true;
        showShell();
        return;
      }
      const result = await bridge.send<AuthResult>("auth.verifyPin", {
        pin: authPin,
      });
      if (result.token) bridge.setAuthToken(result.token);
      showShell();
    } catch (error) {
      await refreshAuthStatus();
      authMessage =
        authRetryAfter > 0
          ? lockoutMessage()
          : error instanceof Error
            ? error.message
            : String(error);
      resetAuthEntry();
      authPad = shuffleDigits();
    } finally {
      authBusy = false;
    }
  }

  $: if (authRetryAfter > 0 && !authRetryTimer) {
    authRetryTimer = window.setInterval(() => {
      authRetryAfter = Math.max(0, authRetryAfter - 1);
      authMessage = lockoutMessage();
      if (authRetryAfter === 0 && authRetryTimer) {
        window.clearInterval(authRetryTimer);
        authRetryTimer = undefined;
      }
    }, 1000);
  }
  $: authEntry =
    authMode === "setup" && authSetupStep === "confirm"
      ? authConfirmPin
      : authPin;
  $: authEntryLength = authEntry.length;
  $: authDotIndexes = Array.from(
    { length: Math.max(authEntryLength, 6) },
    (_, index) => index,
  );
  $: authSubmitLabel = authBusy
    ? "Checking..."
    : authMode === "setup" && authSetupStep === "create"
      ? "Continue"
      : "Enter";

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

  function openAppFromHome(appId: string) {
    appOpenNonce += 1;
    appOpenRequest = { appId, nonce: appOpenNonce };
    section = "apps";
  }

  function openStore(kind: "apps" | "plugins") {
    storeKind = kind;
  }

  function storeChanged() {
    void refreshSubtitles();
    void refreshExtensions();
  }

  function pluginActionIcon(_action: YuiPluginShellAction) {
    return BlocksIcon;
  }

  function pluginActions(location: string): ActionItem[] {
    return (extensions.actions ?? [])
      .filter((action) => action.location === location && action.command)
      .map((action) => ({
        label: action.title,
        icon: pluginActionIcon(action),
        tone: "violet",
        run: () => plugins.run(action.pluginId, action.command ?? ""),
      }));
  }

  $: pluginSections = (extensions.pages ?? []).map((page) => ({
    id: page.id,
    label: page.title,
    subtitle: page.pluginId,
    icon: BlocksIcon,
  })) satisfies SectionItem[];
  $: sections = [...routes, ...pluginSections] satisfies SectionItem[];
  $: activeRoute =
    pluginSections.find((route) => route.id === section) ?? findRoute(section);
  $: activeSubtitle = resolveSubtitle(activeRoute, { appCount, pluginCount });
  $: shellRendered = open || closing || alwaysOpen() || authVisible;
  $: shellClosing = closing && !open && !alwaysOpen();
  $: showTitleBar = !(section === "apps" && appRouteActive);
  $: if (
    bridgeState === "online" &&
    alwaysOpen() &&
    !authUnlocked &&
    !authVisible
  ) {
    void requestAdminEntry();
  }
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
    ...pluginActions("home"),
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

  {#if authVisible}
    <section
      class="auth-panel"
      role="dialog"
      aria-modal="true"
      aria-labelledby="auth-title"
    >
      <div class="auth-header">
        <span class="auth-icon"><LockIcon size={20} strokeWidth={2.4} /></span>
        <div>
          <h2 id="auth-title">
            {authMode === "setup" ? "Set PIN" : "Enter PIN"}
          </h2>
          <p>
            {authMode === "setup"
              ? authSetupStep === "confirm"
                ? "Please confirm the PIN you just entered."
                : "Use 6 digits."
              : "The keypad reshuffles after every tap."}
          </p>
        </div>
      </div>

      <div class="auth-dots" aria-label="PIN entry">
        {#each authDotIndexes as index}
          <span class:filled={index < authEntryLength}></span>
        {/each}
      </div>

      {#if authMessage}
        <p class="auth-message">{authMessage}</p>
      {/if}

      <div class="auth-pad" aria-label="Randomized PIN keypad">
        {#each authPad as digit}
          <button
            class="auth-key"
            disabled={authBusy || authRetryAfter > 0}
            aria-label={`Digit ${digit}`}
            on:click={() => enterDigit(digit)}
          >
            {digit}
          </button>
        {/each}
        <button
          class="auth-key utility"
          disabled={authBusy || authEntryLength < 6}
          on:click={clearAuthPin}
        >
          Clear
        </button>
        <button
          class="auth-key utility"
          disabled={authBusy || authEntryLength === 0}
          aria-label="Delete digit"
          on:click={deleteDigit}
        >
          <DeleteIcon size={18} strokeWidth={2.4} />
        </button>
      </div>

      <button
        class="auth-submit"
        disabled={authBusy || authRetryAfter > 0 || authEntryLength < 6}
        on:click={submitAuth}
      >
        {authSubmitLabel}
      </button>
    </section>
  {:else if authUnlocked}
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
            >
              <svelte:fragment slot="actions">
                {#if section === "home"}
                  <button
                    class:active={homeEditing}
                    class="icon-button title-action-button"
                    aria-label={homeEditing
                      ? "Done editing widgets"
                      : "Edit widgets"}
                    title={homeEditing ? "Done" : "Edit widgets"}
                    on:click={() => (homeEditing = !homeEditing)}
                  >
                    {#if homeEditing}
                      <CheckIcon size={18} strokeWidth={2.4} />
                    {:else}
                      <EditIcon size={18} strokeWidth={2.4} />
                    {/if}
                  </button>
                {:else if section === "apps"}
                  <button
                    class="icon-button title-action-button"
                    aria-label="Open app store"
                    title="Open app store"
                    on:click={() => openStore("apps")}
                  >
                    <PlusIcon size={18} strokeWidth={2.4} />
                  </button>
                {:else if section === "plugins"}
                  <button
                    class="icon-button title-action-button"
                    aria-label="Open plugin store"
                    title="Open plugin store"
                    on:click={() => openStore("plugins")}
                  >
                    <PlusIcon size={18} strokeWidth={2.4} />
                  </button>
                {/if}
              </svelte:fragment>
            </TitleBar>
          {/if}
          <Router
            {section}
            {homeActions}
            {settingDetails}
            {config}
            {appOpenRequest}
            bind:homeEditing
            {pluginSettingsRequest}
            pluginPages={extensions.pages}
            on:appLaunched={(event) => (appRouteActive = event.detail.active)}
            on:launchApp={(event) => openAppFromHome(event.detail.appId)}
            on:navigate={(event) => (section = event.detail.section)}
            on:pluginSettings={(event) =>
              openPluginSettings(event.detail.pluginId)}
            on:pluginSettingsBack={() => (section = "plugins")}
            on:store={(event) => openStore(event.detail.kind)}
          />
        </main>
      </div>
    </section>
  {/if}

  {#if storeKind}
    <FederatedStoreModal
      kind={storeKind}
      on:close={() => (storeKind = null)}
      on:changed={storeChanged}
    />
  {/if}

  {#if permissionPrompt}
    <div class="permission-scrim" role="presentation">
      <section
        class="permission-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="permission-title"
      >
        <div>
          <h2 id="permission-title">
            {permissionPrompt.app.name} wants access
          </h2>
          <p>You can change this later in Settings.</p>
        </div>
        <PermissionCards
          permissions={permissionPrompt.permissions ?? [
            permissionPrompt.permission,
          ]}
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
