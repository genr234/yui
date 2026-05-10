<script lang="ts">
  import ArrowLeftRightIcon from "lucide-svelte/icons/arrow-left-right";
  import CheckIcon from "lucide-svelte/icons/check";
  import ChevronLeftIcon from "lucide-svelte/icons/chevron-left";
  import ChevronRightIcon from "lucide-svelte/icons/chevron-right";
  import ClipboardPasteIcon from "lucide-svelte/icons/clipboard-paste";
  import CornerDownLeftIcon from "lucide-svelte/icons/corner-down-left";
  import DeleteIcon from "lucide-svelte/icons/delete";
  import GripHorizontalIcon from "lucide-svelte/icons/grip-horizontal";
  import UploadIcon from "lucide-svelte/icons/upload";
  import XIcon from "lucide-svelte/icons/x";
  import { onDestroy, onMount } from "svelte";

  type TextTarget = HTMLInputElement | HTMLTextAreaElement;
  type KeyAction =
    | "backspace"
    | "caps"
    | "done"
    | "enter"
    | "left"
    | "right"
    | "shift"
    | "space"
    | "symbols"
    | "tab";
  type KeySpec =
    | string
    | {
        label: string;
        value?: string;
        action?: KeyAction;
        wide?: boolean;
        grow?: boolean;
        icon?: typeof DeleteIcon;
      };

  let root: HTMLDivElement;
  let target: TextTarget | null = null;
  let visible = false;
  let shifted = false;
  let caps = false;
  let symbols = false;
  let left = 0;
  let top = 0;
  let panelWidth = 0;
  let panelHeight = 0;
  let dragged = false;
  let dragOffsetX = 0;
  let dragOffsetY = 0;
  let resizeStartX = 0;
  let resizeStartY = 0;
  let resizeStartWidth = 0;
  let resizeStartHeight = 0;
  let clipboardBusy = false;

  const textInputTypes = new Set([
    "",
    "email",
    "password",
    "search",
    "tel",
    "text",
    "url",
  ]);

  const letterRows: KeySpec[][] = [
    ["q", "w", "e", "r", "t", "y", "u", "i", "o", "p"],
    ["a", "s", "d", "f", "g", "h", "j", "k", "l"],
    [
      { label: "Shift", action: "shift", wide: true },
      "z",
      "x",
      "c",
      "v",
      "b",
      "n",
      "m",
      { label: ",", value: "," },
      { label: ".", value: "." },
    ],
  ];

  const symbolRows: KeySpec[][] = [
    ["1", "2", "3", "4", "5", "6", "7", "8", "9", "0"],
    ["@", "#", "$", "_", "&", "-", "+", "(", ")"],
    [
      { label: "ABC", action: "symbols", wide: true },
      "*",
      '"',
      "'",
      ":",
      ";",
      "!",
      "?",
      { label: "/", value: "/" },
      { label: "\\", value: "\\" },
    ],
  ];

  $: rows = symbols ? symbolRows : letterRows;

  function minPanelWidth() {
    return Math.min(620, window.innerWidth - 32);
  }

  function maxPanelWidth() {
    return Math.max(minPanelWidth(), window.innerWidth - 32);
  }

  function minPanelHeight() {
    return 300;
  }

  function maxPanelHeight() {
    return Math.max(minPanelHeight(), window.innerHeight - 32);
  }

  function clamp(value: number, min: number, max: number) {
    return Math.max(min, Math.min(max, value));
  }

  function isTextTarget(value: EventTarget | null): value is TextTarget {
    if (
      !(
        value instanceof HTMLInputElement ||
        value instanceof HTMLTextAreaElement
      )
    )
      return false;
    if (value.disabled || value.readOnly) return false;
    if (value instanceof HTMLTextAreaElement) return true;
    return textInputTypes.has(value.type);
  }

  function showFor(nextTarget: TextTarget) {
    target = nextTarget;
    visible = true;
    if (!panelWidth || !panelHeight) {
      panelWidth = Math.min(1090, window.innerWidth - 32);
      panelHeight = Math.min(350, window.innerHeight - 32);
    }
    if (!dragged) placeNearTarget();
  }

  function placeNearTarget() {
    left = Math.max(16, (window.innerWidth - panelWidth) / 2);
    top = Math.max(16, window.innerHeight - panelHeight - 16);
  }

  function hide(blurTarget = false) {
    if (blurTarget) target?.blur();
    visible = false;
    shifted = false;
    target = null;
  }

  function handleFocusIn(event: FocusEvent) {
    if (isTextTarget(event.target)) showFor(event.target);
  }

  function handleFocusOut() {
    window.setTimeout(() => {
      const nodeRoot = root.getRootNode();
      const active =
        nodeRoot instanceof ShadowRoot
          ? nodeRoot.activeElement
          : document.activeElement;
      if (!isTextTarget(active)) hide();
    }, 0);
  }

  function dispatchInput() {
    target?.dispatchEvent(
      new Event("input", { bubbles: true, composed: true }),
    );
  }

  function dispatchChange() {
    target?.dispatchEvent(
      new Event("change", { bubbles: true, composed: true }),
    );
  }

  function dispatchKey(key: string, code = key) {
    target?.dispatchEvent(
      new KeyboardEvent("keydown", {
        bubbles: true,
        composed: true,
        key,
        code,
        shiftKey: shifted || caps,
      }),
    );
  }

  function insertText(text: string) {
    if (!target) return;
    target.focus();
    const start = target.selectionStart ?? target.value.length;
    const end = target.selectionEnd ?? start;
    target.value = `${target.value.slice(0, start)}${text}${target.value.slice(end)}`;
    const nextPosition = start + text.length;
    target.setSelectionRange(nextPosition, nextPosition);
    dispatchInput();
    if (shifted && !caps) shifted = false;
  }

  function backspace() {
    if (!target) return;
    target.focus();
    const start = target.selectionStart ?? target.value.length;
    const end = target.selectionEnd ?? start;
    if (start === 0 && end === 0) return;
    const deleteStart = start === end ? start - 1 : start;
    target.value = `${target.value.slice(0, deleteStart)}${target.value.slice(end)}`;
    target.setSelectionRange(deleteStart, deleteStart);
    dispatchInput();
  }

  async function copyText() {
    if (!target || !navigator.clipboard) return;
    const start = target.selectionStart ?? 0;
    const end = target.selectionEnd ?? 0;
    const text = start !== end ? target.value.slice(start, end) : target.value;
    if (!text) return;
    clipboardBusy = true;
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // Browser policy can block clipboard writes outside trusted contexts.
    } finally {
      clipboardBusy = false;
    }
  }

  async function pasteText() {
    if (!target || !navigator.clipboard) return;
    clipboardBusy = true;
    try {
      const text = await navigator.clipboard.readText();
      if (text) insertText(text);
    } catch {
      // Browser policy can block clipboard reads outside trusted contexts.
    } finally {
      clipboardBusy = false;
    }
  }

  function moveCursor(delta: number) {
    if (!target) return;
    target.focus();
    const position = target.selectionStart ?? target.value.length;
    const nextPosition = Math.max(
      0,
      Math.min(target.value.length, position + delta),
    );
    target.setSelectionRange(nextPosition, nextPosition);
  }

  function pressAction(action: KeyAction) {
    if (!target) return;
    if (action === "backspace") {
      dispatchKey("Backspace");
      backspace();
    } else if (action === "caps") {
      caps = !caps;
      shifted = false;
    } else if (action === "done") {
      dispatchChange();
      hide(true);
    } else if (action === "enter") {
      dispatchKey("Enter");
      if (target instanceof HTMLTextAreaElement) insertText("\n");
    } else if (action === "left") {
      moveCursor(-1);
    } else if (action === "right") {
      moveCursor(1);
    } else if (action === "shift") {
      if (shifted) {
        caps = !caps;
        shifted = false;
      } else {
        shifted = true;
      }
    } else if (action === "space") {
      insertText(" ");
    } else if (action === "symbols") {
      symbols = !symbols;
      shifted = false;
    } else if (action === "tab") {
      insertText("\t");
    }
  }

  function pressKey(key: KeySpec) {
    const spec = typeof key === "string" ? { label: key, value: key } : key;
    if (spec.action) {
      pressAction(spec.action);
      return;
    }
    const raw = spec.value ?? spec.label;
    insertText(raw.length === 1 && (shifted || caps) ? raw.toUpperCase() : raw);
  }

  function keyLabel(key: KeySpec) {
    const spec = typeof key === "string" ? { label: key } : key;
    if (spec.label.length === 1 && !symbols) {
      return shifted || caps ? spec.label.toUpperCase() : spec.label;
    }
    return spec.label;
  }

  function startDrag(event: PointerEvent) {
    dragged = true;
    dragOffsetX = event.clientX - left;
    dragOffsetY = event.clientY - top;
    window.addEventListener("pointermove", drag);
    window.addEventListener("pointerup", stopDrag, { once: true });
  }

  function drag(event: PointerEvent) {
    left = Math.max(
      16,
      Math.min(
        window.innerWidth - panelWidth - 16,
        event.clientX - dragOffsetX,
      ),
    );
    top = Math.max(
      16,
      Math.min(
        window.innerHeight - panelHeight - 16,
        event.clientY - dragOffsetY,
      ),
    );
  }

  function stopDrag() {
    window.removeEventListener("pointermove", drag);
  }

  function startResize(event: PointerEvent) {
    resizeStartX = event.clientX;
    resizeStartY = event.clientY;
    resizeStartWidth = panelWidth;
    resizeStartHeight = panelHeight;
    window.addEventListener("pointermove", resize);
    window.addEventListener("pointerup", stopResize, { once: true });
  }

  function resize(event: PointerEvent) {
    panelWidth = clamp(
      resizeStartWidth + event.clientX - resizeStartX,
      minPanelWidth(),
      maxPanelWidth() - left + 16,
    );
    panelHeight = clamp(
      resizeStartHeight + event.clientY - resizeStartY,
      minPanelHeight(),
      maxPanelHeight() - top + 16,
    );
  }

  function stopResize() {
    window.removeEventListener("pointermove", resize);
  }

  function handleResize() {
    panelWidth = clamp(panelWidth || 0, minPanelWidth(), maxPanelWidth());
    panelHeight = clamp(panelHeight || 0, minPanelHeight(), maxPanelHeight());
    if (target && visible && !dragged) placeNearTarget();
  }

  onMount(() => {
    const nodeRoot = root.getRootNode();
    nodeRoot.addEventListener("focusin", handleFocusIn);
    nodeRoot.addEventListener("focusout", handleFocusOut);
    window.addEventListener("resize", handleResize);
    return () => {
      nodeRoot.removeEventListener("focusin", handleFocusIn);
      nodeRoot.removeEventListener("focusout", handleFocusOut);
      window.removeEventListener("resize", handleResize);
    };
  });

  onDestroy(() => {
    window.removeEventListener("pointermove", drag);
    window.removeEventListener("pointermove", resize);
  });
</script>

<div bind:this={root} class="keyboard-anchor" aria-live="polite">
  {#if visible}
    <section
      class="onscreen-keyboard"
      style={`left: ${left}px; top: ${top}px; width: ${panelWidth}px; height: ${panelHeight}px;`}
      aria-label="On-screen keyboard"
      on:pointerdown|preventDefault
    >
      <header class="keyboard-header">
        <div class="keyboard-clipboard-tools">
          <button
            class="keyboard-tool-button"
            type="button"
            aria-label="Copy field text to clipboard"
            disabled={clipboardBusy}
            on:click={copyText}
          >
            <UploadIcon size={19} strokeWidth={2.4} />
          </button>
          <button
            class="keyboard-tool-button"
            type="button"
            aria-label="Paste from clipboard"
            disabled={clipboardBusy}
            on:click={pasteText}
          >
            <ClipboardPasteIcon size={19} strokeWidth={2.4} />
          </button>
        </div>
        <button
          class="keyboard-drag-grip"
          type="button"
          aria-label="Move keyboard"
          on:pointerdown={startDrag}
        >
          <GripHorizontalIcon size={22} strokeWidth={2.4} />
        </button>
      </header>

      <div class="keyboard-body">
        <div class="keyboard-key-deck">
          {#each rows as row}
            <div
              class="keyboard-row"
              style={`grid-template-columns: repeat(${row.length}, minmax(0, 1fr));`}
            >
              {#each row as key}
                <button
                  class:wide={typeof key !== "string" && key.wide}
                  class:grow={typeof key !== "string" && key.grow}
                  class:active={(typeof key !== "string" &&
                    key.action === "shift" &&
                    (shifted || caps)) ||
                    (typeof key !== "string" &&
                      key.action === "symbols" &&
                      symbols)}
                  class="keyboard-key"
                  type="button"
                  on:click={() => pressKey(key)}
                >
                  {#if typeof key !== "string" && key.icon}
                    <svelte:component
                      this={key.icon}
                      size={22}
                      strokeWidth={2.4}
                    />
                  {:else}
                    {keyLabel(key)}
                  {/if}
                </button>
              {/each}
            </div>
          {/each}
        </div>

        <div class="keyboard-action-rail">
          <button
            class="keyboard-side-key"
            type="button"
            aria-label="Backspace"
            on:click={() => pressAction("backspace")}
          >
            <DeleteIcon size={24} strokeWidth={2.4} />
          </button>
          <button
            class="keyboard-side-key"
            type="button"
            aria-label="Enter"
            on:click={() => pressAction("enter")}
          >
            <CornerDownLeftIcon size={26} strokeWidth={2.4} />
          </button>
          <button
            class="keyboard-side-key"
            type="button"
            aria-label="Done"
            on:click={() => pressAction("done")}
          >
            <CheckIcon size={30} strokeWidth={2.5} />
          </button>
        </div>
      </div>

      <footer class="keyboard-footer">
        <button
          class="keyboard-mode-key"
          class:active={symbols}
          type="button"
          on:click={() => pressAction("symbols")}
        >
          {symbols ? "ABC" : "?123"}
        </button>
        <button
          class="keyboard-nav-key"
          type="button"
          aria-label="Tab"
          on:click={() => pressAction("tab")}
        >
          <ArrowLeftRightIcon size={22} strokeWidth={2.5} />
        </button>
        <button
          class="keyboard-nav-key keyboard-punctuation-key"
          type="button"
          aria-label="Comma"
          on:click={() => insertText(",")}
        >
          ,
        </button>
        <button
          class="keyboard-spacebar"
          type="button"
          aria-label="Space"
          on:click={() => pressAction("space")}
        ></button>
        <button
          class="keyboard-nav-key keyboard-punctuation-key"
          type="button"
          aria-label="Period"
          on:click={() => insertText(".")}
        >
          .
        </button>
        <button
          class="keyboard-nav-key"
          type="button"
          aria-label="Move cursor left"
          on:click={() => pressAction("left")}
        >
          <ChevronLeftIcon size={30} strokeWidth={2.5} />
        </button>
        <button
          class="keyboard-nav-key"
          type="button"
          aria-label="Move cursor right"
          on:click={() => pressAction("right")}
        >
          <ChevronRightIcon size={30} strokeWidth={2.5} />
        </button>
      </footer>

      <button
        class="keyboard-resize-handle"
        type="button"
        aria-label="Resize keyboard"
        on:pointerdown={startResize}
      ></button>
    </section>
  {/if}
</div>
