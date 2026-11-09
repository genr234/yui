// Retro Arcade's kiosk page: draws the picker, runs games in play.html and
// turns phone input frames into EmulatorJS input. The app (app.yui.js)
// owns state and talks to this page over the embed channel.
import { createInputMapper, fetchLabel, systemFor, tileSvg } from "./systems.js";

const $ = (id) => document.getElementById(id);
const yui = window.yui;

let view = { phase: "lobby", games: [], recent: [], filter: "all", players: [] };
let ports = {}; // player id -> port
let mappers = new Map(); // port -> input mapper
let game = null; // { frame, window, system, started, paused }
let preparing = false; // the loading screen is up before the game's files are here
let badgeTimer = 0;
let toastTimer = 0;

// ---- channel ------------------------------------------------------------

yui.on((message) => {
  if (!message || typeof message !== "object") return;
  switch (message.type) {
    case "view":
      view = message.view;
      render();
      break;
    case "prepare":
      prepare(message.game, message.label);
      break;
    case "progress":
      if (preparing) $("loading-label").textContent = String(message.label || "");
      break;
    case "load":
      load(message);
      break;
    case "ports":
      setPorts(message.ports || {});
      break;
    case "pause":
      pause(message.reason || "", message.who || null);
      break;
    case "resume":
      resume(Boolean(message.countdown));
      break;
    case "save_state":
      reply("state", message.id, () => game?.window.arcade.state());
      break;
    case "get_sram":
      reply("sram", message.id, () => game?.window.arcade.sram());
      break;
    case "load_state":
      if (game?.started) game.window.arcade.loadState(message.data);
      break;
    case "reset":
      if (game?.started) game.window.arcade.reset();
      break;
    case "stop":
      unprepare();
      stop();
      yui.post({ type: "stopped" });
      break;
    case "toast":
      toast(message.text);
      break;
  }
});

function reply(type, id, read) {
  let data = null;
  try {
    data = read() || null;
  } catch (error) {
    console.warn("[retro-arcade]", type, error);
  }
  // Copy out of the emulator's heap before transferring.
  const bytes = data ? new Uint8Array(data) : null;
  yui.post({ type, id, data: bytes }, bytes ? [bytes.buffer] : []);
}

yui.onInput((frame) => {
  if (!game?.started || game.paused) return;
  const port = ports[frame.player?.id];
  if (port === undefined) return;
  let mapper = mappers.get(port);
  if (!mapper) {
    mapper = createInputMapper(game.system, { rewind: game.rewind });
    mappers.set(port, mapper);
  }
  for (const [index, value] of mapper.update(frame)) game.window.arcade.input(port, index, value);
});

function releaseAll() {
  if (!game?.started) return;
  for (const [port, mapper] of mappers) for (const [index, value] of mapper.release()) game.window.arcade.input(port, index, value);
}

function setPorts(next) {
  const moved = Object.keys(ports).some((id) => ports[id] !== next[id]) || Object.keys(next).some((id) => ports[id] !== next[id]);
  if (moved) {
    releaseAll();
    mappers = new Map();
    showBadges();
  }
  ports = next;
}

// ---- picker -------------------------------------------------------------

function tileFor(entry, extra = "") {
  const button = document.createElement("button");
  button.type = "button";
  button.className = `tile${view.hover === entry.id ? " hover" : ""}`;
  button.dataset.id = entry.id;
  const cart = document.createElement("span");
  cart.className = "cart";
  const image = document.createElement("img");
  image.alt = "";
  image.src = entry.cover || tileSvg(entry.title, entry.systemName);
  image.onerror = () => {
    image.src = tileSvg(entry.title, entry.systemName);
  };
  cart.append(image);
  if (entry.cover) {
    const system = document.createElement("span");
    system.className = "system";
    system.textContent = entry.systemName;
    cart.append(system);
  }
  if (entry.available === false) {
    button.classList.add("not-here");
    const fetch = document.createElement("span");
    fetch.className = "fetch";
    fetch.textContent = entry.state === "remote" ? (entry.error ? "!" : "⇣") : `${Math.round((entry.progress || 0) * 100)}%`;
    fetch.title = fetchLabel(entry);
    cart.append(fetch);
  }
  if (entry.saved) {
    const badge = document.createElement("span");
    badge.className = "badge";
    badge.textContent = "SAVED";
    cart.append(badge);
  }
  const name = document.createElement("span");
  name.className = "name";
  name.textContent = entry.title;
  const meta = document.createElement("span");
  meta.className = "meta";
  meta.textContent = extra || (entry.available === false && entry.state !== "remote" ? `${entry.systemName} · ${fetchLabel(entry)}` : entry.systemName);
  button.append(cart, name, meta);
  button.addEventListener("click", () => yui.post({ type: "pick", id: entry.id }));
  return button;
}

function shelf(title, entries) {
  const section = document.createElement("section");
  section.className = "shelf";
  const heading = document.createElement("h2");
  heading.textContent = title;
  const grid = document.createElement("div");
  grid.className = "grid";
  for (const entry of entries) grid.append(tileFor(entry));
  section.append(heading, grid);
  return section;
}

function renderPicker() {
  $("hint").textContent = view.hint || "";
  const filters = $("filters");
  filters.replaceChildren();
  const systems = view.systems || [];
  if (systems.length > 1) {
    for (const option of [{ id: "all", name: "All" }, ...systems]) {
      const chip = document.createElement("button");
      chip.type = "button";
      chip.className = "chip";
      chip.textContent = option.name;
      chip.setAttribute("aria-pressed", String(view.filter === option.id));
      chip.addEventListener("click", () => yui.post({ type: "filter", system: option.id }));
      filters.append(chip);
    }
  }
  const games = view.filter && view.filter !== "all" ? view.games.filter((entry) => entry.system === view.filter) : view.games;
  const recent = (view.recent || []).map((id) => games.find((entry) => entry.id === id)).filter(Boolean);
  const shelves = $("shelves");
  shelves.replaceChildren();
  if (recent.length) shelves.append(shelf("RECENTLY PLAYED", recent));
  if (games.length) shelves.append(shelf(recent.length ? "ALL GAMES" : "GAMES", games));

  const empty = $("empty");
  empty.hidden = Boolean(games.length) && !view.empty;
  if (!empty.hidden) {
    empty.replaceChildren();
    const strong = document.createElement("strong");
    strong.textContent = view.empty?.title || "NO GAMES YET";
    empty.append(strong);
    for (const line of view.empty?.lines || []) {
      const p = document.createElement("p");
      p.textContent = line;
      empty.append(p);
    }
  }
}

// ---- games --------------------------------------------------------------

function render() {
  if (preparing && view.phase === "lobby") unprepare();
  const playing = Boolean(game);
  $("app").dataset.phase = view.phase;
  $("picker").hidden = playing;
  $("stage").hidden = !playing;
  if (!playing) renderPicker();
  if (view.error && view.phase === "lobby") showError(view.error);
  else $("error").hidden = true;
  renderBadges();
}

// Shows the loading screen the moment a game is picked, before its files or
// saves are ready; load() takes over from here.
function prepare(entry, label) {
  if (!entry || game) return;
  const system = systemFor(entry.system);
  preparing = true;
  $("loading-tile").src = entry.cover || tileSvg(entry.title, system ? system.name : "");
  $("loading-title").textContent = entry.title;
  $("loading-label").textContent = String(label || "Loading…");
  $("loading").hidden = false;
  $("error").hidden = true;
}

function unprepare() {
  if (!preparing) return;
  preparing = false;
  if (!game) $("loading").hidden = true;
}

function load(message) {
  preparing = false;
  stop();
  const system = systemFor(message.game.system);
  if (!system) {
    showError("This kind of game isn't supported.");
    return;
  }
  $("loading-tile").src = message.game.cover || tileSvg(message.game.title, system.name);
  $("loading-title").textContent = message.game.title;
  $("loading-label").textContent = "Loading the emulator…";
  $("loading").hidden = false;
  $("error").hidden = true;
  $("picker").hidden = true;
  $("stage").hidden = false;

  const frame = document.createElement("iframe");
  frame.title = message.game.title;
  frame.allow = "autoplay; fullscreen; gamepad";
  frame.src = "play.html";
  $("frame-wrap").replaceChildren(frame);
  game = { frame, window: null, system, started: false, paused: false, rewind: Boolean(message.rewind) };
  mappers = new Map();
  const current = game;

  frame.addEventListener("load", () => {
    if (game !== current) return;
    current.window = frame.contentWindow;
    current.window.arcadeEvents.on((event) => {
      if (game !== current) return;
      if (event.type === "progress") $("loading-label").textContent = event.label;
      else if (event.type === "error") {
        yui.post({ type: "error", message: event.message });
        stop();
        showError(event.message);
      } else if (event.type === "started") {
        current.started = true;
        $("loading").hidden = true;
        frame.focus();
        showBadges();
        yui.post({ type: "started" });
      }
    });
    current.window.boot({
      core: system.core,
      gameUrl: message.gameUrl,
      gameName: message.game.title,
      runtimeUrl: message.runtimeUrl,
      state: message.state || null,
      sram: message.sram || null,
      rewind: Boolean(message.rewind),
      crt: Boolean(message.crt),
    });
  });
}

function stop() {
  if (!game) return;
  releaseAll();
  game.frame.remove();
  game = null;
  mappers = new Map();
  $("loading").hidden = true;
  $("paused").hidden = true;
  $("countdown").hidden = true;
  $("stage").hidden = true;
  $("picker").hidden = false;
  renderPicker();
}

function pause(reason, who) {
  if (!game) return;
  releaseAll();
  game.paused = true;
  if (game.started) game.window.arcade.pause(true);
  $("paused-text").textContent = who ? `Waiting for ${who.name}…` : "Paused";
  $("paused-sub").textContent = reason;
  fillAvatar($("paused-avatar"), who);
  $("paused-avatar").hidden = !who;
  $("paused").hidden = false;
  $("countdown").hidden = true;
}

let countdownRun = 0;
async function resume(countdown) {
  if (!game) return;
  $("paused").hidden = true;
  const run = ++countdownRun;
  if (countdown) {
    $("countdown").hidden = false;
    for (const n of [3, 2, 1]) {
      const el = $("countdown-n");
      el.textContent = String(n);
      // Restart the pop animation.
      el.style.animation = "none";
      void el.offsetWidth;
      el.style.animation = "";
      await new Promise((resolve) => setTimeout(resolve, 900));
      if (run !== countdownRun || !game) return;
    }
    $("countdown").hidden = true;
  }
  game.paused = false;
  if (game.started) game.window.arcade.pause(false);
  game.frame.focus();
}

// ---- small bits ---------------------------------------------------------

function fillAvatar(el, player) {
  el.replaceChildren();
  if (!player) return;
  el.style.background = player.swatch || "#5b4bd6";
  const avatar = String(player.avatar || "");
  if (/^data:image\/(png|jpeg|webp);base64,/.test(avatar)) {
    const img = document.createElement("img");
    img.alt = "";
    img.src = avatar;
    el.append(img);
  } else {
    el.textContent = avatar || String(player.name || "?").slice(0, 1).toUpperCase();
  }
}

function renderBadges() {
  const box = $("badges");
  box.replaceChildren();
  const seated = (view.players || []).filter((player) => ports[player.id] !== undefined).sort((a, b) => ports[a.id] - ports[b.id]);
  for (const player of seated) {
    const pill = document.createElement("span");
    pill.className = "badge-pill";
    const avatar = document.createElement("span");
    avatar.className = "avatar";
    fillAvatar(avatar, player);
    const port = document.createElement("b");
    port.textContent = `P${ports[player.id] + 1}`;
    pill.append(avatar, port, document.createTextNode(player.name));
    box.append(pill);
  }
}

function showBadges() {
  renderBadges();
  const box = $("badges");
  box.classList.remove("faded");
  clearTimeout(badgeTimer);
  badgeTimer = setTimeout(() => box.classList.add("faded"), 3000);
}

function showError(text) {
  $("error-text").textContent = text;
  $("error").hidden = false;
  $("loading").hidden = true;
}

$("error-back").addEventListener("click", () => {
  $("error").hidden = true;
  yui.post({ type: "dismiss_error" });
});

function toast(text) {
  const el = $("toast");
  el.textContent = String(text || "");
  el.hidden = !text;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (el.hidden = true), 2600);
}

render();
yui.ready.then(() => yui.post({ type: "page_ready" }));
