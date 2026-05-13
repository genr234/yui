<script lang="ts">
  import PlayIcon from "lucide-svelte/icons/play";
  import {
    plugins,
    type YuiPluginPageBlock,
    type YuiPluginShellPage,
  } from "../sdk/plugins";

  export let page: YuiPluginShellPage | null = null;

  let busy = "";
  let output = "";
  let error = "";

  $: rootClass = page ? `yui-plugin-page-${safeClass(page.pluginId)}` : "";
  $: scopedCSS = page ? scopeCSS(page.css ?? "", rootClass) : "";

  function safeClass(value: string) {
    return value.replace(/[^a-zA-Z0-9_-]/g, "-");
  }

  function scopeCSS(css: string, scope: string) {
    return css
      .split("}")
      .map((rule) => {
        const [selectors, body] = rule.split("{");
        if (!selectors || !body) return "";
        const scoped = selectors
          .split(",")
          .map((selector) => `.${scope} ${selector.trim()}`)
          .join(", ");
        return `${scoped} {${body}}`;
      })
      .join("\n");
  }

  async function run(command: string | undefined) {
    if (!page || !command) return;
    busy = command;
    error = "";
    output = "";
    try {
      const result = await plugins.run(page.pluginId, command);
      output = JSON.stringify(result, null, 2);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = "";
    }
  }

  function blockText(block: YuiPluginPageBlock) {
    return String(block.body ?? block.value ?? "");
  }
</script>

<svelte:head>
  <style>
{scopedCSS}
  </style>
</svelte:head>

{#if page}
  <section class={`settings-stack plugin-extension-page ${rootClass}`}>
    <div class="settings-page">
      <section class="settings-app-hero">
        <span class="app-icon"
          ><span>{page.icon ?? page.title.slice(0, 2)}</span></span
        >
        <div>
          <h2>{page.title}</h2>
          <p>{page.pluginId}</p>
        </div>
      </section>

      <section class="settings-group">
        {#each page.blocks ?? [] as block}
          {#if block.type === "heading"}
            <div class="settings-row static">
              <span>
                <span>{block.title ?? block.label}</span>
                {#if block.body}<small>{block.body}</small>{/if}
              </span>
            </div>
          {:else if block.type === "stat"}
            <div class="settings-row static settings-actions-row">
              <span>
                <span>{block.label ?? block.title}</span>
                <small>{String(block.value ?? "")}</small>
              </span>
            </div>
          {:else if block.type === "code"}
            <div class="settings-row static">
              <span>
                <span>{block.title ?? "Output"}</span>
                <small class="plugin-code-block">{blockText(block)}</small>
              </span>
            </div>
          {:else if block.type === "button"}
            <div class="settings-row static settings-actions-row">
              <span>
                <span>{block.title ?? block.label}</span>
                {#if block.body}<small>{block.body}</small>{/if}
              </span>
              <button
                class="settings-inline-button"
                disabled={Boolean(busy) || !block.command}
                on:click={() => run(block.command)}
              >
                <PlayIcon size={14} strokeWidth={2.4} />
                {block.label ?? "Run"}
              </button>
            </div>
          {:else}
            <div class="settings-row static">
              <span>
                <span>{block.title ?? block.label ?? "Text"}</span>
                <small>{blockText(block)}</small>
              </span>
            </div>
          {/if}
        {/each}
      </section>

      {#if error || output}
        <section class="settings-group">
          <div class="settings-row static">
            <span>
              <span>{error ? "Plugin error" : "Command result"}</span>
              <small class="plugin-code-block">{error || output}</small>
            </span>
          </div>
        </section>
      {/if}
    </div>
  </section>
{/if}
