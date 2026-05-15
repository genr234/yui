<script lang="ts">
  import { createEventDispatcher, onMount } from "svelte";
  import ChevronRightIcon from "lucide-svelte/icons/chevron-right";
  import ArrowLeftIcon from "lucide-svelte/icons/arrow-left";
  import DownloadIcon from "lucide-svelte/icons/download";
  import RefreshCwIcon from "lucide-svelte/icons/refresh-cw";
  import TrashIcon from "lucide-svelte/icons/trash-2";
  import UserCircleIcon from "lucide-svelte/icons/user-circle";
  import PermissionCards from "../components/PermissionCards.svelte";
  import Plugins from "./Plugins.svelte";
  import {
    fallbackAppIcon,
    findApp,
    firstAvailableAppId,
    isImageAppIcon,
    loadAppsLibrary,
    uninstallApp as uninstallInstalledApp,
  } from "../apps";
  import { type YuiDevApp } from "../sdk/apps";
  import { bridge } from "../sdk/bridge";
  import {
    declaredPermissions,
    describePermission,
    getAppPermissionState,
    setAppPermission,
    setPermissionAccountScope,
  } from "../sdk/apps/permissions";
  import {
    clearEmbedStorage,
    getEmbedStorageEntries,
    type EmbedStorageEntry,
  } from "../sdk/apps/embed-storage";
  import { clearAppStorage, listAppStorageKeys } from "../sdk/apps/app-storage";
  import type { DetailItem } from "../types";

  export let details: DetailItem[] = [];
  export let config: any = null;
  export let pluginSettingsRequest: { pluginId: string; nonce: number } | null =
    null;
  export let accountOpenRequest: { nonce: number } | null = null;

  const dispatch = createEventDispatcher<{
    pluginSettingsBack: void;
    store: { kind: "plugins" };
    accountChanged: void;
  }>();

  type UpdateStatus = {
    enabled: boolean;
    repo: string;
    current_commit: string;
    latest_commit: string;
    latest_tag: string;
    latest_url: string;
    update_available: boolean;
    checked_at: string;
    error?: string;
  };

  let apps: YuiDevApp[] = [];
  let selectedId = "";
  let page: "root" | "apps" | "app" | "plugins" | "update" | "account" = "root";
  let pageDirection: "forward" | "back" = "forward";
  let hasNavigated = false;
  let dragging = false;
  let dragStartX = 0;
  let dragX = 0;
  let permissionVersion = 0;
  let embedStorageVersion = 0;
  let appStorageVersion = 0;
  let loadedStorageKey = "";
  let selectedEmbedStorage: EmbedStorageEntry[] = [];
  let selectedStorageKeys: string[] = [];
  let updateStatus: UpdateStatus | null = null;
  let updateBusy = false;
  let updateApplying = false;
  let updateMessage = "";
  let updateError = "";
  let appBusy = "";
  let appMessage = "";
  let appError = "";
  let pluginPageTitle = "Plugins";
  let pluginPageCanGoBack = false;
  let pluginPageBack: (() => void) | null = null;
  let forwardedPluginRequest: { pluginId: string; nonce: number } | null = null;
  let consumedPluginRequestNonce = 0;
  let consumedAccountRequestNonce = 0;
  let returnToPluginsTab = false;
  let accountStatus: AccountStatus | null = null;
  let accountBusy = "";
  let accountMessage = "";
  let accountError = "";
  let serverUrl = "http://127.0.0.1:3000";
  let pairingCode = "";
  let kioskName = "Yui kiosk";

  type AccountInfo = {
    id: string;
    name: string;
    profile_image_url?: string;
    kiosk_id: string;
    sync_cursor: number;
  };

  type AccountStatus = {
    server_url?: string;
    connected: boolean;
    needs_pairing: boolean;
    anonymous: boolean;
    syncing: boolean;
    last_sync_at?: string;
    last_sync_error?: string;
    active_account?: AccountInfo;
    accounts: AccountInfo[];
  };

  onMount(() => {
    const refresh = () => {
      permissionVersion += 1;
    };
    const refreshEmbedStorage = () => {
      embedStorageVersion += 1;
    };
    const refreshAppStorage = () => {
      appStorageVersion += 1;
    };
    const refreshApps = () => {
      void loadAppsArea();
    };
    window.addEventListener("yui:permissions-changed", refresh);
    window.addEventListener("yui:embed-storage-changed", refreshEmbedStorage);
    window.addEventListener("yui:app-storage-changed", refreshAppStorage);
    window.addEventListener("yui:apps-changed", refreshApps);

    void (async () => {
      await loadAppsArea();
    })();
    void loadUpdateStatus(false);
    void loadAccountStatus();

    return () => {
      window.removeEventListener("yui:permissions-changed", refresh);
      window.removeEventListener(
        "yui:embed-storage-changed",
        refreshEmbedStorage,
      );
      window.removeEventListener("yui:app-storage-changed", refreshAppStorage);
      window.removeEventListener("yui:apps-changed", refreshApps);
    };
  });

  $: selected = findApp(apps, selectedId) ?? apps[0];
  $: if (
    pluginSettingsRequest &&
    pluginSettingsRequest.nonce !== consumedPluginRequestNonce
  ) {
    consumedPluginRequestNonce = pluginSettingsRequest.nonce;
    forwardedPluginRequest = pluginSettingsRequest;
    returnToPluginsTab = true;
    goTo("plugins", "forward");
  }
  $: if (
    accountOpenRequest &&
    accountOpenRequest.nonce !== consumedAccountRequestNonce
  ) {
    consumedAccountRequestNonce = accountOpenRequest.nonce;
    goTo("account", "forward");
  }
  $: selectedPermissions = selected ? declaredPermissions(selected.app) : [];
  $: selectedPermissionState = selected
    ? getAppPermissionState(selected.id)
    : {};
  $: {
    const storageKey = selected
      ? `${selected.id}:${embedStorageVersion}:${appStorageVersion}`
      : "";
    if (storageKey !== loadedStorageKey) {
      loadedStorageKey = storageKey;
      void loadSelectedStorage();
    }
  }

  async function loadAppsArea() {
    const library = await loadAppsLibrary();
    apps = library.apps;
    selectedId = firstAvailableAppId(apps, selectedId);
  }

  async function uninstallApp(id: string) {
    appBusy = `uninstall:${id}`;
    appError = "";
    appMessage = "";
    try {
      await uninstallInstalledApp(id);
      appMessage = "App uninstalled.";
      await loadAppsArea();
    } catch (error) {
      appError = error instanceof Error ? error.message : String(error);
    } finally {
      appBusy = "";
    }
  }

  async function loadSelectedStorage() {
    if (!selected) {
      selectedEmbedStorage = [];
      selectedStorageKeys = [];
      return;
    }

    const app = selected;
    const [embedStorage, storageKeys] = await Promise.all([
      getEmbedStorageEntries(app.id),
      listAppStorageKeys(app.id),
    ]);

    if (selected?.id !== app.id) return;
    selectedEmbedStorage = embedStorage;
    selectedStorageKeys = storageKeys;
  }

  function permissionGranted(permission: string) {
    permissionVersion;
    return selectedPermissionState[permission] === true;
  }

  function togglePermission(permission: string, checked: boolean) {
    if (!selected) return;
    setAppPermission(selected.id, permission, checked);
    permissionVersion += 1;
  }

  function setAllSelectedPermissions(granted: boolean) {
    if (!selected) return;
    for (const permission of selectedPermissions) {
      setAppPermission(selected.id, permission, granted);
    }
    permissionVersion += 1;
  }

  async function clearEmbedOrigin(origin?: string) {
    if (!selected) return;
    await clearEmbedStorage(selected.id, origin);
    embedStorageVersion += 1;
  }

  async function clearSelectedAppStorage() {
    if (!selected) return;
    const appId = selected.id;
    await clearAppStorage(appId);
    window.dispatchEvent(
      new CustomEvent("yui:embed-storage-cleared", {
        detail: { appId },
      }),
    );
    appStorageVersion += 1;
    embedStorageVersion += 1;
  }

  function openApp(app: YuiDevApp) {
    selectedId = app.id;
    goTo("app", "forward");
  }

  function goTo(
    nextPage: "root" | "apps" | "app" | "plugins" | "update" | "account",
    direction: "forward" | "back",
  ) {
    pageDirection = direction;
    hasNavigated = true;
    page = nextPage;
    dragX = 0;
  }

  function previousPage() {
    if (page === "app") return "apps";
    if (page === "plugins") return pluginPageCanGoBack ? "plugins" : "root";
    if (page === "account") return "root";
    if (page === "update") return "root";
    if (page === "apps") return "root";
    return "root";
  }

  function handlePluginPage(
    event: CustomEvent<{ title: string; canGoBack: boolean; back: () => void }>,
  ) {
    pluginPageTitle = event.detail.title;
    pluginPageCanGoBack = event.detail.canGoBack;
    pluginPageBack = event.detail.back;
  }

  function backFromPlugins() {
    if (pluginPageCanGoBack) {
      if (returnToPluginsTab) {
        returnToPluginsTab = false;
        forwardedPluginRequest = null;
        dispatch("pluginSettingsBack");
        return;
      }
      pluginPageBack?.();
      return;
    }
    goTo("root", "back");
  }

  async function loadUpdateStatus(showBusy = true) {
    if (showBusy) updateBusy = true;
    updateError = "";
    updateMessage = "";
    try {
      updateStatus = await bridge.send<UpdateStatus>("update.check");
    } catch (error) {
      updateError = error instanceof Error ? error.message : String(error);
    } finally {
      updateBusy = false;
    }
  }

  async function setAutoUpdateEnabled(enabled: boolean) {
    updateBusy = true;
    updateError = "";
    updateMessage = "";
    try {
      config = await bridge.send("config.update", {
        auto_update_enabled: enabled,
      });
      updateStatus = {
        ...(updateStatus ?? {
          repo: config?.auto_update_repo ?? "genr234/yui",
          current_commit: "unknown",
          latest_commit: "",
          latest_tag: "",
          latest_url: "",
          update_available: false,
          checked_at: "",
        }),
        enabled,
      };
      if (enabled) await loadUpdateStatus(false);
    } catch (error) {
      updateError = error instanceof Error ? error.message : String(error);
    } finally {
      updateBusy = false;
    }
  }

  async function applyUpdate() {
    updateApplying = true;
    updateError = "";
    updateMessage = "";
    try {
      updateStatus = await bridge.send<UpdateStatus>("update.apply");
      updateMessage = updateStatus.update_available
        ? "Installer started. Yui will close and restart after the update."
        : "Yui is already on the latest kiosk build.";
    } catch (error) {
      updateError = error instanceof Error ? error.message : String(error);
    } finally {
      updateApplying = false;
    }
  }

  function startPageDrag(event: PointerEvent) {
    if (page === "root") return;
    if (event.pointerType === "mouse" && event.button !== 0) return;
    dragging = true;
    dragStartX = event.clientX;
    dragX = 0;
  }

  function movePageDrag(event: PointerEvent) {
    if (!dragging) return;
    dragX = Math.max(0, Math.min(event.clientX - dragStartX, 180));
  }

  function endPageDrag(event: PointerEvent) {
    if (!dragging) return;
    dragging = false;

    if (Math.max(dragX, event.clientX - dragStartX) > 72) {
      goTo(previousPage(), "back");
    } else {
      dragX = 0;
    }
  }

  function shortCommit(value?: string) {
    if (!value) return "Unknown";
    return value.length > 12 ? value.slice(0, 12) : value;
  }

  function autoUpdateEnabled() {
    return config?.auto_update_enabled !== false;
  }

  function updateSummary() {
    if (!autoUpdateEnabled()) return "Off";
    if (updateStatus?.update_available) {
      return `Update ready: ${shortCommit(updateStatus.latest_commit)}`;
    }
    if (updateStatus?.latest_commit) return "Latest build installed";
    return "On";
  }

  async function loadAccountStatus() {
    const result = await bridge
      .send<AccountStatus>("accounts.status")
      .catch((error) => {
        accountError = error instanceof Error ? error.message : String(error);
        return null;
      });
    if (!result) return;
    accountStatus = result;
    setPermissionAccountScope(result.active_account?.id ?? null);
    serverUrl = result.server_url || serverUrl;
    accountError = result.last_sync_error ?? "";
  }

  function accountSummary() {
    if (accountStatus?.syncing) return "Syncing";
    if (accountStatus?.last_sync_error) return "Sync error";
    if (accountStatus?.needs_pairing) return "Reconnect";
    if (accountStatus?.active_account) return accountStatus.active_account.name;
    return "Anonymous";
  }

  function accountConnectionDetail() {
    if (accountStatus?.needs_pairing) return "Pair this kiosk again";
    if (accountStatus?.server_url) return accountStatus.server_url;
    return "Local data only";
  }

  function accountLastSync() {
    if (!accountStatus?.last_sync_at) return "Not synced yet";
    return new Date(accountStatus.last_sync_at).toLocaleString();
  }

  async function runAccountAction(
    busy: string,
    action: () => Promise<void>,
    message: string,
  ) {
    accountBusy = busy;
    accountError = "";
    accountMessage = "";
    try {
      await action();
      accountMessage = message;
      await loadAccountStatus();
      resetAccountScopedSelections();
      await loadAppsArea();
      notifyAccountDataChanged();
      dispatch("accountChanged");
    } catch (error) {
      accountError = error instanceof Error ? error.message : String(error);
    } finally {
      accountBusy = "";
    }
  }

  function resetAccountScopedSelections() {
    selectedId = "";
    loadedStorageKey = "";
    selectedEmbedStorage = [];
    selectedStorageKeys = [];
    permissionVersion += 1;
    embedStorageVersion += 1;
    appStorageVersion += 1;
  }

  function notifyAccountDataChanged() {
    window.dispatchEvent(new CustomEvent("yui:account-changed"));
    window.dispatchEvent(new CustomEvent("yui:apps-changed"));
    window.dispatchEvent(new CustomEvent("yui:plugins-changed"));
    window.dispatchEvent(new CustomEvent("yui:permissions-changed"));
    window.dispatchEvent(new CustomEvent("yui:app-storage-changed"));
    window.dispatchEvent(new CustomEvent("yui:embed-storage-changed"));
  }

  async function connectAccount() {
    await runAccountAction(
      "connect",
      async () => {
        await bridge.send("accounts.connect", {
          server_url: serverUrl,
          code: pairingCode,
          name: kioskName,
        });
        pairingCode = "";
      },
      "Account connected.",
    );
  }

  async function switchAccount(accountId: string) {
    await runAccountAction(
      `switch:${accountId || "anonymous"}`,
      async () => {
        await bridge.send("accounts.switch", { account_id: accountId });
      },
      accountId ? "Account switched." : "Using anonymous data.",
    );
  }

  async function syncAccount() {
    await runAccountAction(
      "sync",
      async () => {
        await bridge.send("accounts.syncNow");
      },
      "Sync finished.",
    );
  }

  async function importAnonymous() {
    await runAccountAction(
      "import",
      async () => {
        await bridge.send("accounts.importAnonymous");
      },
      "Anonymous data imported.",
    );
  }

  async function disconnectAccount() {
    await runAccountAction(
      "disconnect",
      async () => {
        await bridge.send("accounts.disconnect");
      },
      "Server disconnected.",
    );
  }
</script>

<section
  class:dragging
  class="settings-stack"
  style={`--settings-drag-x: ${dragX}px`}
  on:pointerdown={startPageDrag}
  on:pointermove={movePageDrag}
  on:pointerup={endPageDrag}
  on:pointercancel={endPageDrag}
>
  {#key page}
    {#if page === "root"}
      <div
        class:settings-page-motion={hasNavigated}
        class="settings-page {pageDirection}"
      >
        <section class="settings-group">
          <button class="settings-row" on:click={() => goTo("apps", "forward")}>
            <span>
              <span>Apps</span>
              <small
                >{apps.length === 1 ? "1 app" : `${apps.length} apps`}</small
              >
            </span>
            <ChevronRightIcon size={18} strokeWidth={2.4} />
          </button>
          <button
            class="settings-row"
            on:click={() => goTo("update", "forward")}
          >
            <span>
              <span>Auto update</span>
              <small>{updateSummary()}</small>
            </span>
            <ChevronRightIcon size={18} strokeWidth={2.4} />
          </button>
          <button
            class="settings-row"
            on:click={() => goTo("account", "forward")}
          >
            <span>
              <span>Account</span>
              <small>{accountSummary()}</small>
            </span>
            <ChevronRightIcon size={18} strokeWidth={2.4} />
          </button>
          <button
            class="settings-row"
            on:click={() => goTo("plugins", "forward")}
          >
            <span>
              <span>Plugins</span>
              <small>Permissions, settings, storage</small>
            </span>
            <ChevronRightIcon size={18} strokeWidth={2.4} />
          </button>
        </section>

        <section class="settings-group">
          {#each details as detail}
            <div class="settings-row static">
              <span>
                <p>{detail.label}</p>
              </span>
              <small>{detail.value}</small>
            </div>
          {/each}
        </section>
      </div>
    {:else if page === "account"}
      <div
        class:settings-page-motion={hasNavigated}
        class="settings-page {pageDirection}"
      >
        <nav class="settings-nav" aria-label="Settings navigation">
          <button
            class="icon-button"
            aria-label="Back to settings"
            title="Back to settings"
            on:click={() => goTo("root", "back")}
          >
            <ArrowLeftIcon size={18} strokeWidth={2.4} />
          </button>
          <span>Account</span>
        </nav>

        <section class="settings-app-hero">
          <span class="app-icon">
            {#if accountStatus?.active_account?.profile_image_url}
              <img
                src={accountStatus.active_account.profile_image_url}
                alt=""
              />
            {:else}
              <UserCircleIcon size={30} strokeWidth={1.9} />
            {/if}
          </span>
          <div>
            <h2>{accountStatus?.active_account?.name ?? "Anonymous"}</h2>
            <p>{accountConnectionDetail()}</p>
            <small>
              {accountStatus?.needs_pairing
                ? "Device token missing"
                : accountStatus?.syncing
                  ? "Syncing"
                  : `Last sync: ${accountLastSync()}`}
            </small>
          </div>
        </section>

        <section class="settings-group">
          <div class="settings-row static">
            <span>
              <span>Server</span>
              <small>{accountStatus?.server_url || "Not connected"}</small>
            </span>
          </div>
          <div class="settings-row static">
            <span>
              <span>Active data</span>
              <small
                >{accountStatus?.active_account?.id ??
                  "Anonymous local store"}</small
              >
            </span>
          </div>
          <div class="settings-row static settings-actions-row">
            <span>
              <span>Sync</span>
              <small>
                {accountStatus?.needs_pairing
                  ? "Create a new pairing code in the backend and connect again."
                  : accountStatus?.last_sync_error ||
                    accountMessage ||
                    "Push and pull account changes."}
              </small>
            </span>
            <button
              class="settings-inline-button"
              disabled={!accountStatus?.connected ||
                accountStatus?.needs_pairing ||
                Boolean(accountBusy)}
              type="button"
              on:click={syncAccount}
            >
              <RefreshCwIcon size={14} strokeWidth={2.4} />
              Sync
            </button>
          </div>
        </section>

        <section class="settings-group">
          <form class="account-form" on:submit|preventDefault={connectAccount}>
            <label>
              <span>Backend URL</span>
              <input
                class="settings-text-input"
                bind:value={serverUrl}
                disabled={Boolean(accountBusy)}
                placeholder="http://127.0.0.1:3000"
              />
            </label>
            <label>
              <span>Pairing code</span>
              <input
                class="settings-text-input"
                bind:value={pairingCode}
                disabled={Boolean(accountBusy)}
                inputmode="numeric"
                placeholder="123456"
              />
            </label>
            <label>
              <span>Kiosk name</span>
              <input
                class="settings-text-input"
                bind:value={kioskName}
                disabled={Boolean(accountBusy)}
              />
            </label>
            <div class="account-form-actions">
              <button
                class="settings-inline-button"
                disabled={Boolean(accountBusy) || !serverUrl || !pairingCode}
                type="submit"
              >
                Connect
              </button>
            </div>
          </form>
        </section>

        {#if (accountStatus?.accounts ?? []).length > 0}
          <section class="settings-group">
            <button
              class="settings-row"
              class:active-account={accountStatus?.anonymous}
              disabled={Boolean(accountBusy)}
              on:click={() => switchAccount("")}
            >
              <span>
                <span>Anonymous</span>
                <small>Local data only</small>
              </span>
              <small>{accountStatus?.anonymous ? "Active" : "Switch"}</small>
            </button>
            {#each accountStatus?.accounts ?? [] as account}
              <button
                class="settings-row"
                class:active-account={accountStatus?.active_account?.id ===
                  account.id}
                disabled={Boolean(accountBusy)}
                on:click={() => switchAccount(account.id)}
              >
                <span>
                  <span>{account.name}</span>
                  <small>Kiosk {account.kiosk_id}</small>
                </span>
                <small
                  >{accountStatus?.active_account?.id === account.id
                    ? "Active"
                    : "Switch"}</small
                >
              </button>
            {/each}
          </section>
        {/if}

        <section class="settings-group">
          <div class="settings-row static settings-actions-row">
            <span>
              <span>Anonymous data</span>
              <small
                >Copy local apps, plugins, and storage into this account.</small
              >
            </span>
            <button
              class="settings-inline-button"
              disabled={!accountStatus?.connected ||
                accountStatus?.needs_pairing ||
                Boolean(accountBusy)}
              type="button"
              on:click={importAnonymous}
            >
              Import
            </button>
          </div>
          <div class="settings-row static settings-actions-row">
            <span>
              <span>Server connection</span>
              <small>Disconnect this kiosk from the backend.</small>
            </span>
            <button
              class="settings-inline-button danger"
              disabled={!accountStatus?.connected ||
                accountStatus?.needs_pairing ||
                Boolean(accountBusy)}
              type="button"
              on:click={disconnectAccount}
            >
              Disconnect
            </button>
          </div>
        </section>

        {#if accountError}
          <p class="settings-message error">{accountError}</p>
        {:else if accountMessage}
          <p class="settings-message">{accountMessage}</p>
        {/if}
      </div>
    {:else if page === "update"}
      <div
        class:settings-page-motion={hasNavigated}
        class="settings-page {pageDirection}"
      >
        <nav class="settings-nav" aria-label="Settings navigation">
          <button
            class="icon-button"
            aria-label="Back to settings"
            title="Back to settings"
            on:click={() => goTo("root", "back")}
          >
            <ArrowLeftIcon size={18} strokeWidth={2.4} />
          </button>
          <span>Auto update</span>
        </nav>

        <section class="settings-group">
          <label class="settings-row permission-row">
            <span>
              <span>Install latest kiosk builds</span>
              <small>{config?.auto_update_repo ?? "genr234/yui"}</small>
            </span>
            <input
              type="checkbox"
              checked={autoUpdateEnabled()}
              disabled={updateBusy || updateApplying}
              on:change={(event) =>
                setAutoUpdateEnabled(event.currentTarget.checked)}
            />
          </label>
          <div class="settings-row static">
            <span>
              <span>Current commit</span>
            </span>
            <small>{shortCommit(updateStatus?.current_commit)}</small>
          </div>
          <div class="settings-row static">
            <span>
              <span>Latest commit</span>
              <small>{updateStatus?.latest_tag || "Not checked yet"}</small>
            </span>
            <small>{shortCommit(updateStatus?.latest_commit)}</small>
          </div>
        </section>

        <section class="settings-group">
          <div class="settings-row static settings-actions-row">
            <span>
              <span>Release check</span>
              <small>
                {#if updateError}
                  {updateError}
                {:else if updateMessage}
                  {updateMessage}
                {:else if updateStatus?.update_available}
                  A newer kiosk installer is ready.
                {:else}
                  {updateBusy ? "Checking GitHub..." : "No newer build found."}
                {/if}
              </small>
            </span>
            <span class="settings-actions">
              <button
                class="settings-inline-button"
                disabled={updateBusy || updateApplying}
                title="Check for updates"
                on:click={() => loadUpdateStatus(true)}
              >
                <RefreshCwIcon size={14} strokeWidth={2.4} />
                Check
              </button>
              <button
                class="settings-inline-button"
                disabled={!updateStatus?.update_available || updateApplying}
                title="Install latest build"
                on:click={applyUpdate}
              >
                <DownloadIcon size={14} strokeWidth={2.4} />
                Install
              </button>
            </span>
          </div>
        </section>
      </div>
    {:else if page === "apps"}
      <div
        class:settings-page-motion={hasNavigated}
        class="settings-page {pageDirection}"
      >
        <nav class="settings-nav" aria-label="Settings navigation">
          <button
            class="icon-button"
            aria-label="Back to settings"
            title="Back to settings"
            on:click={() => goTo("root", "back")}
          >
            <ArrowLeftIcon size={18} strokeWidth={2.4} />
          </button>
          <span>Apps</span>
        </nav>

        {#if apps.length === 0}
          <section class="settings-group">
            <div class="settings-row static">
              <span>
                <span>No apps found</span>
                <small
                  >Local app permissions will appear here after app discovery
                  finds a yui manifest.</small
                >
              </span>
            </div>
          </section>
        {:else}
          <section class="settings-group">
            {#each apps as app}
              <button
                class="settings-row app-row"
                on:click={() => openApp(app)}
              >
                <span class="app-icon">
                  {#if isImageAppIcon(app.app.icon)}
                    <img src={app.app.icon} alt="" class="app-row-icon-img" />
                  {:else}
                    <span>{fallbackAppIcon(app)}</span>
                  {/if}
                </span>
                <span>
                  <span>{app.name}</span>
                  <small
                    >{app.id}{app.installed
                      ? " · installed"
                      : " · local"}</small
                  >
                </span>
                <ChevronRightIcon size={18} strokeWidth={2.4} />
              </button>
            {/each}
          </section>
        {/if}
      </div>
    {:else if page === "plugins"}
      <div
        class:settings-page-motion={hasNavigated}
        class="settings-page {pageDirection}"
      >
        <nav class="settings-nav" aria-label="Settings navigation">
          <button
            class="icon-button"
            aria-label={pluginPageCanGoBack
              ? "Back to plugins"
              : "Back to settings"}
            title={pluginPageCanGoBack ? "Back to plugins" : "Back to settings"}
            on:click={backFromPlugins}
          >
            <ArrowLeftIcon size={18} strokeWidth={2.4} />
          </button>
          <span>{pluginPageTitle}</span>
        </nav>

        <Plugins
          mode="manage"
          embedded
          hideNav
          openPluginRequest={forwardedPluginRequest}
          on:page={handlePluginPage}
          on:store={() => dispatch("store", { kind: "plugins" })}
        />
      </div>
    {:else}
      <div
        class:settings-page-motion={hasNavigated}
        class="settings-page {pageDirection}"
      >
        <nav class="settings-nav" aria-label="Settings navigation">
          <button
            class="icon-button"
            aria-label="Back to apps"
            title="Back to apps"
            on:click={() => goTo("apps", "back")}
          >
            <ArrowLeftIcon size={18} strokeWidth={2.4} />
          </button>
          <span>{selected?.name ?? "App"}</span>
        </nav>

        {#if selected}
          <section class="settings-app-hero">
            <span class="app-icon">
              {#if isImageAppIcon(selected.app.icon)}
                <img src={selected.app.icon} alt="" />
              {:else}
                <span>{fallbackAppIcon(selected)}</span>
              {/if}</span
            >
            <div>
              <h2>{selected.name}</h2>
              <p>{selected.app.description ?? selected.id}</p>
              <small>{selected.version}</small>
              {#if selected.installed}
                <small>Installed from {selected.sourceUrl}</small>
              {/if}
            </div>
          </section>

          {#if selected.installed}
            <section class="settings-group">
              <div class="settings-row static settings-actions-row">
                <span>
                  <span>Installed app</span>
                  <small
                    >Uninstalling keeps this app's isolated storage until you
                    clear it.</small
                  >
                </span>
                <button
                  class="settings-inline-button danger"
                  disabled={Boolean(appBusy)}
                  type="button"
                  on:click={() => uninstallApp(selected.id)}
                >
                  <TrashIcon size={14} strokeWidth={2.4} />
                  Uninstall
                </button>
              </div>
            </section>
          {/if}

          <section class="settings-group">
            {#if selectedPermissions.length === 0}
              <div class="settings-row static">
                <span>
                  <span>No permissions</span>
                  <small>This app declares no permissions.</small>
                </span>
              </div>
            {:else}
              <div class="settings-row static">
                <PermissionCards
                  permissions={selectedPermissions}
                  granted={selectedPermissions.filter(permissionGranted)}
                  describe={describePermission}
                  onToggle={togglePermission}
                />
              </div>
            {/if}
          </section>

          <section class="settings-group">
            <div class="settings-row static">
              <span>
                <span>App storage</span>
                <small>
                  {selectedStorageKeys.length === 1
                    ? "1 isolated record"
                    : `${selectedStorageKeys.length} isolated records`}
                </small>
              </span>
              {#if selectedStorageKeys.length > 0}
                <button
                  class="settings-inline-button danger"
                  type="button"
                  on:click={clearSelectedAppStorage}
                >
                  Clear
                </button>
              {/if}
            </div>
          </section>

          <section class="settings-group">
            {#if selectedEmbedStorage.length === 0}
              <div class="settings-row static">
                <span>
                  <span>Embedded websites</span>
                  <small>No embedded websites tracked for this app.</small>
                </span>
              </div>
            {:else}
              <div class="settings-row static">
                <span>
                  <span>Embedded websites</span>
                  <small
                    >Credentialless embeds are isolated from the normal web
                    profile. Clearing forgets this app's tracked websites and
                    reloads active embeds.</small
                  >
                </span>
                <button
                  class="settings-inline-button danger"
                  type="button"
                  on:click={() => clearEmbedOrigin()}
                >
                  Clear all
                </button>
              </div>
              {#each selectedEmbedStorage as entry}
                <div class="settings-row static">
                  <span>
                    <span>{entry.origin}</span>
                    <small
                      >Last used {new Date(
                        entry.lastUsedAt,
                      ).toLocaleString()}</small
                    >
                  </span>
                  <button
                    class="settings-inline-button"
                    type="button"
                    on:click={() => clearEmbedOrigin(entry.origin)}
                  >
                    Clear
                  </button>
                </div>
              {/each}
            {/if}
          </section>
        {/if}
      </div>
    {/if}
  {/key}
</section>
