<script lang="ts">
  import { onMount } from "svelte";
  import ChevronRightIcon from "lucide-svelte/icons/chevron-right";
  import ArrowLeftIcon from "lucide-svelte/icons/arrow-left";
  import { discoverDevApps, type YuiDevApp } from "../sdk/apps";
  import {
    declaredPermissions,
    describePermission,
    getAppPermissionState,
    setAppPermission,
  } from "../sdk/apps/permissions";
  import type { DetailItem } from "../types";

  export let details: DetailItem[] = [];

  let apps: YuiDevApp[] = [];
  let selectedId = "";
  let page: "root" | "apps" | "app" = "root";
  let pageDirection: "forward" | "back" = "forward";
  let dragging = false;
  let dragStartX = 0;
  let dragX = 0;
  let permissionVersion = 0;

  onMount(() => {
    const refresh = () => {
      permissionVersion += 1;
    };
    window.addEventListener("yui:permissions-changed", refresh);

    void (async () => {
      apps = await discoverDevApps();
      selectedId = apps[0]?.id ?? "";
    })();

    return () => window.removeEventListener("yui:permissions-changed", refresh);
  });

  $: selected = apps.find((app) => app.id === selectedId) ?? apps[0];
  $: selectedPermissions = selected ? declaredPermissions(selected.app) : [];
  $: selectedPermissionState = selected
    ? getAppPermissionState(selected.id)
    : {};

  function permissionGranted(permission: string) {
    permissionVersion;
    return selectedPermissionState[permission] === true;
  }

  function togglePermission(permission: string, checked: boolean) {
    if (!selected) return;
    setAppPermission(selected.id, permission, checked);
    permissionVersion += 1;
  }

  function openApp(app: YuiDevApp) {
    selectedId = app.id;
    goTo("app", "forward");
  }

  function goTo(
    nextPage: "root" | "apps" | "app",
    direction: "forward" | "back",
  ) {
    pageDirection = direction;
    page = nextPage;
    dragX = 0;
  }

  function previousPage() {
    if (page === "app") return "apps";
    if (page === "apps") return "root";
    return "root";
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

  function isImageIcon(icon?: string) {
    return Boolean(
      icon &&
        (/^(https?:|data:|\/|\.)/.test(icon) ||
          /\.(png|jpe?g|gif|webp|svg)$/i.test(icon)),
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
      <div class="settings-page settings-page-motion {pageDirection}">
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
    {:else if page === "apps"}
      <div class="settings-page settings-page-motion {pageDirection}">
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
                  {#if isImageIcon(app.app.icon)}
                    <img src={app.app.icon} alt="" class="app-row-icon-img" />
                  {:else}
                    <span>{app.app.icon ?? app.name.slice(0, 4)}</span>
                  {/if}
                </span>
                <span>
                  <span>{app.name}</span>
                  <small>{app.id}</small>
                </span>
                <ChevronRightIcon size={18} strokeWidth={2.4} />
              </button>
            {/each}
          </section>
        {/if}
      </div>
    {:else}
      <div class="settings-page settings-page-motion {pageDirection}">
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
              {#if isImageIcon(selected.app.icon)}
                <img src={selected.app.icon} alt="" />
              {:else}
                <span>{selected.app.icon ?? selected.name.slice(0, 4)}</span>
              {/if}</span
            >
            <div>
              <h2>{selected.name}</h2>
              <p>{selected.app.description ?? selected.id}</p>
              <small>{selected.version}</small>
            </div>
          </section>

          <section class="settings-group">
            {#if selectedPermissions.length === 0}
              <div class="settings-row static">
                <span>
                  <span>No permissions</span>
                  <small>This app declares no permissions.</small>
                </span>
              </div>
            {:else}
              {#each selectedPermissions as permission}
                <label class="settings-row permission-row">
                  <span>
                    <span>{describePermission(permission).label}</span>
                    <small>{describePermission(permission).description}</small>
                  </span>
                  <input
                    type="checkbox"
                    checked={permissionGranted(permission)}
                    on:change={(event) =>
                      togglePermission(permission, event.currentTarget.checked)}
                  />
                </label>
              {/each}
            {/if}
          </section>
        {/if}
      </div>
    {/if}
  {/key}
</section>
