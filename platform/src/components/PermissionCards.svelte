<script lang="ts">
  import BellIcon from "lucide-svelte/icons/bell";
  import ClipboardIcon from "lucide-svelte/icons/clipboard";
  import CodeIcon from "lucide-svelte/icons/code-2";
  import DatabaseIcon from "lucide-svelte/icons/database";
  import EyeIcon from "lucide-svelte/icons/eye";
  import FileIcon from "lucide-svelte/icons/file";
  import FolderIcon from "lucide-svelte/icons/folder";
  import GlobeIcon from "lucide-svelte/icons/globe";
  import KeyIcon from "lucide-svelte/icons/key-round";
  import MaximizeIcon from "lucide-svelte/icons/maximize";
  import PlayIcon from "lucide-svelte/icons/play";
  import RadioIcon from "lucide-svelte/icons/radio";
  import SettingsIcon from "lucide-svelte/icons/settings";
  import ShieldIcon from "lucide-svelte/icons/shield";
  import TerminalIcon from "lucide-svelte/icons/terminal";

  export let permissions: string[] = [];
  export let granted: string[] = [];
  export let describe: (
    permission: string,
  ) => { label: string; description: string };
  export let readonly = false;
  export let disabled = false;
  export let onToggle: (
    permission: string,
    granted: boolean,
  ) => void = () => {};

  $: grantedSet = new Set(granted);

  function iconFor(permission: string) {
    if (permission.includes("network") || permission.startsWith("embed:"))
      return GlobeIcon;
    if (permission.includes("storage")) return DatabaseIcon;
    if (permission.includes("clipboard")) return ClipboardIcon;
    if (permission.includes("notification")) return BellIcon;
    if (permission.includes("fullscreen")) return MaximizeIcon;
    if (permission.includes("command")) return CodeIcon;
    if (permission.includes("secret")) return KeyIcon;
    if (permission.includes("setting") || permission.includes("config"))
      return SettingsIcon;
    if (permission.includes("fs.read")) return FileIcon;
    if (permission.includes("fs.write") || permission.includes("fs.list"))
      return FolderIcon;
    if (permission.includes("process")) return PlayIcon;
    if (permission.includes("shell")) return TerminalIcon;
    if (permission.includes("event")) return RadioIcon;
    if (permission.includes("status") || permission.includes("read"))
      return EyeIcon;
    return ShieldIcon;
  }
</script>

<div>
  {#each permissions as permission}
    {@const copy = describe(permission)}
    {@const Icon = iconFor(permission)}
    {#if readonly}
      <div class:readonly class="permission-card">
        <span class="permission-card-icon">
          <Icon size={18} strokeWidth={2.4} />
        </span>
        <span class="permission-card-copy">
          <span>{copy.label}</span>
          <small>{copy.description}</small>
        </span>
        <small
          class:allowed={grantedSet.has(permission)}
          class="permission-state"
        >
          {grantedSet.has(permission) ? "Allowed" : "Denied"}
        </small>
      </div>
    {:else}
      <label class="permission-card">
        <span class="permission-card-icon">
          <Icon size={18} strokeWidth={2.4} />
        </span>
        <span class="permission-card-copy">
          <span>{copy.label}</span>
          <small>{copy.description}</small>
        </span>
        <input
          type="checkbox"
          checked={grantedSet.has(permission)}
          {disabled}
          on:change={(event) =>
            onToggle(permission, event.currentTarget.checked)}
        />
      </label>
    {/if}
  {/each}
</div>
