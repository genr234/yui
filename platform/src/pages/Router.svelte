<script lang="ts">
  import { createEventDispatcher } from "svelte";
  import type { ActionItem, DetailItem, Section } from "../types";
  import About from "./About.svelte";
  import Apps from "./Apps.svelte";
  import Home from "./Home.svelte";
  import Settings from "./Settings.svelte";
  import Plugins from "./Plugins.svelte";
  import PluginPage from "./PluginPage.svelte";
  import type { YuiPluginShellPage } from "../sdk/plugins";

  export let section: Section = "home";
  export let homeActions: ActionItem[] = [];
  export let settingDetails: DetailItem[] = [];
  export let config: any = null;
  export let appOpenRequest: { appId: string; nonce: number } | null = null;
  export let homeEditing = false;
  export let pluginSettingsRequest: { pluginId: string; nonce: number } | null =
    null;
  export let pluginPages: YuiPluginShellPage[] = [];
  const dispatch = createEventDispatcher<{
    appLaunched: { active: boolean };
    launchApp: { appId: string };
    navigate: { section: Section };
    pluginSettings: { pluginId: string };
    pluginSettingsBack: void;
    store: { kind: "apps" | "plugins" };
  }>();
</script>

<div
  class="router-panel"
  hidden={section !== "home"}
  aria-hidden={section !== "home"}
>
  <Home
    actions={homeActions}
    bind:editing={homeEditing}
    on:launchApp={(event) => dispatch("launchApp", event.detail)}
    on:navigate={(event) => dispatch("navigate", event.detail)}
    on:pluginSettings={(event) => dispatch("pluginSettings", event.detail)}
  />
</div>

<div
  class="router-panel"
  hidden={section !== "apps"}
  aria-hidden={section !== "apps"}
>
  <Apps
    openAppRequest={appOpenRequest}
    on:launched={(event) => dispatch("appLaunched", event.detail)}
    on:store={() => dispatch("store", { kind: "apps" })}
  />
</div>

<div
  class="router-panel"
  hidden={section !== "plugins"}
  aria-hidden={section !== "plugins"}
>
  <Plugins
    on:settings={(event) => dispatch("pluginSettings", event.detail)}
    on:store={() => dispatch("store", { kind: "plugins" })}
  />
</div>

<div
  class="router-panel"
  hidden={section !== "settings"}
  aria-hidden={section !== "settings"}
>
  <Settings
    details={settingDetails}
    {config}
    {pluginSettingsRequest}
    on:pluginSettingsBack={() => dispatch("pluginSettingsBack")}
    on:store={(event) => dispatch("store", event.detail)}
  />
</div>

{#if (pluginPages ?? []).some((page) => page.id === section)}
  <div class="router-panel">
    <PluginPage page={(pluginPages ?? []).find((page) => page.id === section) ?? null} />
  </div>
{/if}

<div
  class="router-panel"
  hidden={section !== "about"}
  aria-hidden={section !== "about"}
>
  <About />
</div>
