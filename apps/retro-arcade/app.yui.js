export default {
  schema: "yui.simple-js.v0",
  id: "dev.genr.retro-arcade",
  name: "Retro Arcade",
  version: "0.1.0",
  icon: "👾",
  category: "Games",
  description: "Classic console games from your library, played in the browser. Every phone is a controller.",
  permissions: ["controllers", "assets:game", "assets:runtime", "library", "saves", "profiles"],
  web: { runtimes: ["emulatorjs"] },

  // Declaring controllers gives this app the phones while it is open.
  controllers: {},

  mount(ctx) {
    const pad = ctx.controllers;
    const errorText = (error) => String((error && error.message) || error || "Something went wrong").replace(/^YUI_[A-Z_]+:\s*/, "");
    const CHANNEL = "main";
    const MAX_PHONE_GAMES = 24;

    const state = ctx.state({
      loaded: false,
      games: [],
      pending: 0,
      runtimeUrl: "",
      runtimeError: "",
      filter: "all",
      search: "",
      recent: [], // asset ids, newest first
      saved: {}, // asset id -> true when a save state exists
      phase: "lobby", // lobby | loading | playing
      current: null,
      ports: {}, // player id -> port
      waitingFor: null, // player id the game paused for
      hostView: "games", // games | load | pass | settings
      loadList: [], // [{ key, label, scope }]
      settings: { crt: false, rewind: false },
      saveOwner: null, // player id whose account keeps this game's saves
      error: "",
      hover: null,
      loadingLabel: "", // download progress while a picked game is fetched
    });

    // ---- the page -----------------------------------------------------------

    let pageReady = false;
    let viewQueued = false;
    const waiters = new Map();
    let requestId = 0;

    function post(message) {
      if (pageReady) ctx.embeds.post(CHANNEL, message);
    }

    function swatch(player) {
      const colors = { blue: "#2f76dc", red: "#e5484d", green: "#30a46c", yellow: "#f5a623", purple: "#8e4ec6", orange: "#f76b15", pink: "#d6409f", teal: "#12a594" };
      return colors[player.color] || "#5b4bd6";
    }

    function emptyMessage() {
      if (!state.loaded) return { title: "LOOKING FOR GAMES", lines: [] };
      const lines = [];
      if (!state.runtimeUrl) lines.push("The EmulatorJS runtime isn't on this kiosk yet. An admin can upload it in the control room (runtime name emulatorjs, version " + EMULATORJS_VERSION + ").");
      if (!state.games.length) {
        lines.push(state.pending ? `${state.pending} game${state.pending === 1 ? " is" : "s are"} still downloading.` : "Add games you own to your library in the control room, or link your RomM library under Account → Linked accounts.");
      }
      return lines.length ? { title: state.games.length ? "ALMOST READY" : "NO GAMES YET", lines } : null;
    }

    function sendView() {
      if (viewQueued) return;
      viewQueued = true;
      queueMicrotask(() => {
        viewQueued = false;
        const systems = [];
        for (const system of SYSTEMS) if (state.games.some((game) => game.system === system.id)) systems.push({ id: system.id, name: system.name });
        const host = pad.players.find((player) => player.host && player.connected);
        post({
          type: "view",
          view: {
            phase: state.phase,
            games: state.games.map((game) => ({ ...game, saved: Boolean(state.saved[game.id]) })),
            recent: state.recent.filter((id) => state.games.some((game) => game.id === id)).slice(0, 6),
            systems,
            filter: state.filter,
            hover: state.hover,
            hint: pad.session ? (host ? `${host.name} picks from their phone, or tap a game.` : "Scan the code to join, or tap a game.") : "Tap a game to play.",
            empty: state.runtimeUrl && state.games.length ? null : emptyMessage(),
            error: state.error,
            players: pad.players.map((player) => ({ id: player.id, name: player.name, color: player.color, swatch: swatch(player), avatar: player.avatar || "" })),
          },
        });
      });
    }

    function request(type, extra = {}) {
      const id = ++requestId;
      return new Promise((resolve) => {
        waiters.set(id, resolve);
        post({ type, id, ...extra });
        setTimeout(() => {
          if (waiters.delete(id)) resolve(null);
        }, 5000);
      });
    }

    ctx.embeds.on(CHANNEL, (message) => {
      if (!message || typeof message !== "object") return;
      switch (message.type) {
        case "page_ready":
          pageReady = true;
          sendView();
          break;
        case "pick":
          pick(message.id);
          break;
        case "filter":
          state.filter = message.system;
          sendView();
          break;
        case "started":
          state.phase = "playing";
          post({ type: "ports", ports: state.ports });
          buzzPorts("success");
          break;
        case "error":
          state.error = message.message || "The game didn't start.";
          backToLobby();
          break;
        case "dismiss_error":
          state.error = "";
          sendView();
          break;
        case "state":
        case "sram": {
          const resolve = waiters.get(message.id);
          if (resolve) {
            waiters.delete(message.id);
            resolve(message.data || null);
          }
          break;
        }
      }
    });

    // ---- library and assets --------------------------------------------------

    let watching = false;
    async function refresh() {
      try {
        const picked = selectGames(await ctx.assets("game"));
        state.games = picked.games;
        state.pending = picked.pending;
        checkFetch();
        if (!watching) {
          watching = true;
          ctx.assets.onChange(() => refresh());
        }
      } catch (error) {
        state.error = `Couldn't read the game library: ${errorText(error)}`;
      }
      try {
        state.runtimeUrl = await ctx.web.runtime(EMULATORJS_RUNTIME);
        state.runtimeError = "";
      } catch (error) {
        state.runtimeUrl = "";
        state.runtimeError = errorText(error);
      }
      state.loaded = true;
      sendView();
      maybeResumeLaunch();
    }

    async function loadHistory() {
      try {
        const titles = await ctx.library.list();
        titles.sort((a, b) => String(b.played_at || "").localeCompare(String(a.played_at || "")));
        state.recent = titles.map((title) => title.resume && title.resume.assetId).filter(Boolean);
      } catch (error) {
        ctx.log.warn("library unavailable", errorText(error));
      }
      try {
        const entries = await ctx.saves.list();
        const saved = {};
        for (const entry of entries) {
          const match = /^(.*)\.state\.\d+$/.exec(entry.key);
          if (match) saved[match[1]] = true;
        }
        const byKey = {};
        for (const game of state.games) if (saved[saveKeyBase(game.id)]) byKey[game.id] = true;
        state.saved = byKey;
      } catch (error) {
        ctx.log.warn("saves unavailable", errorText(error));
      }
      sendView();
    }

    function markPlayed(game) {
      const entry = { id: libraryIdFor(game.id), title: game.title, players: (systemFor(game.system) || { ports: 1 }).ports, art: game.cover && /^https:/.test(game.cover) ? undefined : tileSvg(game.title, game.systemName), resume: { assetId: game.id } };
      if (!entry.art) delete entry.art;
      ctx.library
        .publish([entry])
        .then(() => ctx.library.played(entry.id, { resume: entry.resume }))
        .catch((error) => ctx.log.warn("library update failed", errorText(error)));
      state.recent = [game.id, ...state.recent.filter((id) => id !== game.id)];
    }

    // ---- saves --------------------------------------------------------------

    const saveOptions = () => (state.saveOwner ? { player: state.saveOwner } : undefined);

    async function chooseSaveOwner() {
      state.saveOwner = null;
      const first = Object.keys(state.ports).find((id) => state.ports[id] === 0);
      const player = first && pad.player(first);
      if (!player || !player.accountLinked || !ctx.profiles) return;
      try {
        const { granted } = await ctx.profiles.for(player).request();
        if (granted) state.saveOwner = player.id;
      } catch (error) {
        ctx.log.warn("profile request failed", errorText(error));
      }
    }

    async function listStates(game) {
      const out = [];
      const scopes = state.saveOwner ? [["player", { player: state.saveOwner }], ["kiosk", undefined]] : [["kiosk", undefined]];
      for (const [scope, options] of scopes) {
        try {
          for (const entry of statesFor(game.id, await ctx.saves.list(options))) out.push({ ...entry, scope });
        } catch (error) {
          ctx.log.warn("listing saves failed", errorText(error));
        }
      }
      return out.sort((a, b) => String(b.updatedAt).localeCompare(String(a.updatedAt)));
    }

    async function readBlob(key, scope) {
      try {
        const blob = await ctx.saves.get(key, scope === "player" && state.saveOwner ? { player: state.saveOwner } : undefined);
        return blob ? blob.data : null;
      } catch (error) {
        ctx.log.warn("reading a save failed", errorText(error));
        return null;
      }
    }

    let lastSramHash = "";
    function hashBytes(bytes) {
      let hash = 2166136261;
      for (let i = 0; i < bytes.length; i += 7) hash = Math.imul(hash ^ bytes[i], 16777619) >>> 0;
      return `${bytes.length}:${hash}`;
    }

    async function saveSram() {
      const game = state.current;
      if (!game || state.phase !== "playing") return;
      const data = await request("get_sram");
      if (!data || !data.length) return;
      const hash = hashBytes(data);
      if (hash === lastSramHash) return;
      try {
        await ctx.saves.put(sramKey(game.id), data, saveOptions());
        lastSramHash = hash;
      } catch (error) {
        ctx.log.warn("saving the game's save file failed", errorText(error));
      }
    }

    async function saveState(announce = true) {
      const game = state.current;
      if (!game || state.phase !== "playing") return false;
      const data = await request("save_state");
      if (!data || !data.length) {
        if (announce) toast("Couldn't save the game");
        return false;
      }
      try {
        const existing = await ctx.saves.list(saveOptions());
        await ctx.saves.put(stateKey(game.id, nextStateSlot(game.id, existing)), data, saveOptions());
        state.saved = { ...state.saved, [game.id]: true };
        if (announce) {
          toast("Saved");
          buzzPorts("success");
        }
        return true;
      } catch (error) {
        if (announce) toast(`Couldn't save: ${errorText(error)}`);
        return false;
      }
    }

    setInterval(() => saveSram(), 60_000);

    // ---- starting and stopping ---------------------------------------------

    const gameById = (id) => state.games.find((game) => game.id === String(id)) || null;

    function toast(text) {
      post({ type: "toast", text });
    }

    function buzzPorts(name) {
      for (const id of Object.keys(state.ports)) pad.haptic(id, name).catch(() => {});
    }

    let pendingContinue = null; // a game opened from Jump back in, waiting for the host's choice

    function pick(id) {
      const game = gameById(id);
      if (!game || state.phase !== "lobby") return;
      if (!state.runtimeUrl) {
        state.error = "The EmulatorJS runtime isn't on this kiosk yet.";
        sendView();
        return;
      }
      start(game, { continue: Boolean(state.saved[game.id]) });
    }

    // A game that isn't on this kiosk yet: ask for it once, then follow
    // ctx.assets.onChange until it is ready (checkFetch runs on every refresh).
    let fetching = null; // { id, resolve, reject, requested, deadline }
    function fetchGame(game) {
      return new Promise((resolve, reject) => {
        fetching = { id: game.id, resolve, reject, requested: game.state !== "remote", deadline: Date.now() + 15 * 60_000 };
        if (game.state === "remote") {
          ctx.assets.request(game.id).then(
            (asset) => {
              if (!fetching || fetching.id !== game.id) return;
              fetching.requested = true;
              if (asset && asset.state === "remote" && asset.error) failFetch(`Couldn't download ${game.title}: ${asset.error}`);
              else refresh();
            },
            (error) => failFetch(`Couldn't download ${game.title}: ${errorText(error)}`),
          );
        }
      });
    }

    function failFetch(message) {
      const waiter = fetching;
      fetching = null;
      if (waiter) waiter.reject(new Error(message));
    }

    function checkFetch() {
      if (!fetching) return;
      const game = gameById(fetching.id);
      if (!game) return failFetch("That game was removed from the library.");
      if (game.available) {
        const waiter = fetching;
        fetching = null;
        waiter.resolve(game);
        return;
      }
      if (fetching.requested && game.state === "remote" && game.error) return failFetch(`Couldn't download ${game.title}: ${game.error}`);
      if (Date.now() > fetching.deadline) return failFetch(`${game.title} is taking too long to download. Try again in a bit.`);
      const label = fetchLabel(game);
      if (label !== state.loadingLabel) {
        state.loadingLabel = label;
        post({ type: "progress", label });
      }
    }

    async function start(game, options) {
      const system = systemFor(game.system);
      if (!system || state.phase !== "lobby") return;
      state.phase = "loading";
      state.current = game;
      state.error = "";
      state.hostView = "games";
      state.ports = assignPorts(pad.players, system);
      state.loadingLabel = game.available ? "" : fetchLabel(game);
      lastSramHash = "";
      // Show the loading screen straight away; everything below runs behind it.
      post({
        type: "prepare",
        game: { id: game.id, title: game.title, system: game.system, systemName: game.systemName, cover: game.cover },
        label: game.available ? "Loading the emulator…" : state.loadingLabel,
      });
      sendView();
      const ready = game.available ? Promise.resolve(game) : fetchGame(game);
      ready.catch(() => {}); // handled below; keeps an early failure from going unhandled
      const stillHere = () => state.phase === "loading" && state.current === game;
      try {
        await chooseSaveOwner();
        if (!stillHere()) return;
        const scope = state.saveOwner ? "player" : "kiosk";
        const [sram, saved] = await Promise.all([
          readBlob(sramKey(game.id), scope),
          options.continue
            ? listStates(game).then((states) => (states.length ? readBlob(states[0].key, states[0].scope) : null))
            : null,
          ready,
        ]);
        if (!stillHere()) return;
        state.loadingLabel = "";
        const gameUrl = await ctx.assets.url(game.id);
        if (!stillHere()) return;
        post({
          type: "load",
          game: { id: game.id, title: game.title, system: game.system, systemName: game.systemName, cover: game.cover },
          gameUrl,
          runtimeUrl: state.runtimeUrl,
          state: saved,
          sram,
          rewind: state.settings.rewind,
          crt: state.settings.crt,
        });
        post({ type: "ports", ports: state.ports });
        markPlayed(game);
      } catch (error) {
        if (!stillHere()) return;
        state.error = errorText(error);
        backToLobby();
      }
    }

    function backToLobby() {
      if (fetching) {
        const waiter = fetching;
        fetching = null;
        waiter.reject(new Error("cancelled"));
      }
      state.loadingLabel = "";
      state.phase = "lobby";
      state.current = null;
      state.ports = {};
      state.waitingFor = null;
      state.hostView = "games";
      sendView();
    }

    async function quit() {
      if (state.phase === "lobby") return;
      if (state.phase === "playing") {
        await saveSram();
        await saveState(false);
      }
      post({ type: "stop" });
      backToLobby();
    }

    // Opened from Jump back in: once a host is here, ask Continue or Start over.
    const launch = ctx.launch.item;
    if (launch && launch.resume && launch.resume.assetId) pendingContinue = String(launch.resume.assetId);
    function maybeResumeLaunch() {
      if (!pendingContinue || state.phase !== "lobby" || !state.loaded) return;
      const game = gameById(pendingContinue);
      if (!game) return;
      const host = pad.players.find((player) => player.host && player.connected);
      if (!host) {
        // Nobody to ask: just start it, continuing when there is a save.
        if (!pad.session) {
          pendingContinue = null;
          start(game, { continue: Boolean(state.saved[game.id]) });
        }
        return;
      }
      state.hostView = state.saved[game.id] ? "continue" : "games";
      if (!state.saved[game.id]) {
        pendingContinue = null;
        start(game, { continue: false });
      }
    }

    // ---- players --------------------------------------------------------------

    function syncPorts() {
      if (state.phase === "lobby" || !state.current) return;
      const system = systemFor(state.current.system);
      const next = assignPorts(pad.players, system, state.ports);
      const changed = JSON.stringify(next) !== JSON.stringify(state.ports);
      // Port 1's player dropped: pause until they're back.
      const first = Object.keys(state.ports).find((id) => state.ports[id] === 0);
      const firstPlayer = first && pad.player(first);
      if (state.phase === "playing" && firstPlayer && !firstPlayer.connected) {
        if (state.waitingFor !== first) {
          state.waitingFor = first;
          post({ type: "pause", reason: "Their phone lost its connection.", who: { id: firstPlayer.id, name: firstPlayer.name, swatch: swatch(firstPlayer), avatar: firstPlayer.avatar || "" } });
        }
        return;
      }
      if (state.waitingFor && firstPlayer && firstPlayer.connected) {
        state.waitingFor = null;
        post({ type: "resume", countdown: true });
        buzzPorts("countdown");
      }
      if (changed) {
        state.ports = next;
        post({ type: "ports", ports: next });
      }
    }

    pad.on("players", () => {
      syncPorts();
      maybeResumeLaunch();
      sendView();
    });

    // ---- the phone's Home sheet ------------------------------------------------

    pad.on("system", async ({ player, action }) => {
      if (state.phase !== "playing") return;
      switch (action) {
        case "resume":
          break;
        case "save_state":
          if (player.host || state.ports[player.id] === 0) await saveState(true);
          break;
        case "load_state":
          if (!(player.host || state.ports[player.id] === 0)) return;
          state.loadList = (await listStates(state.current)).slice(0, 3);
          state.hostView = "load";
          break;
        case "quit":
          if (player.host) await quit();
          break;
      }
    });

    // ---- phone layouts -----------------------------------------------------------

    function since(iso) {
      const minutes = Math.round((Date.now() - Date.parse(iso)) / 60000);
      if (!Number.isFinite(minutes) || minutes < 1) return "just now";
      if (minutes < 60) return `${minutes} min ago`;
      const hours = Math.round(minutes / 60);
      if (hours < 24) return `${hours} h ago`;
      return `${Math.round(hours / 24)} d ago`;
    }

    function phoneGames() {
      const query = state.search.trim().toLowerCase();
      let games = state.filter === "all" ? state.games : state.games.filter((game) => game.system === state.filter);
      if (query) games = games.filter((game) => game.title.toLowerCase().includes(query));
      const recent = state.recent.map(gameById).filter((game) => game && games.includes(game));
      const ordered = [...recent, ...games.filter((game) => !recent.includes(game))];
      return ordered.slice(0, MAX_PHONE_GAMES).map((game) => ({
        id: game.id,
        title: game.title,
        subtitle: state.saved[game.id] ? `${game.systemName} · continue` : game.available ? game.systemName : `${game.systemName} · ${fetchLabel(game).toLowerCase()}`,
        image: game.cover && /^https:/.test(game.cover) ? game.cover : tileSvg(game.title, game.systemName),
      }));
    }

    function hostLobby(ui) {
      if (!state.loaded) return ui.text("Looking for games…", { align: "center" });
      if (state.hostView === "continue" && pendingContinue) {
        const game = gameById(pendingContinue);
        return ui.column({ gap: 14 }, [
          ui.text(game ? game.title : "Your game", { size: "large", align: "center" }),
          ui.text("Pick up where you left off?", { align: "center" }),
          ui.button({ id: "continue", label: "Continue", variant: "primary" }),
          ui.button({ id: "fresh", label: "Start over", variant: "secondary" }),
        ]);
      }
      if (!state.runtimeUrl || !state.games.length) {
        const message = emptyMessage();
        return ui.column({ gap: 12 }, [ui.text(message ? message.title : "Not ready", { size: "large", align: "center" }), ...(message ? message.lines.map((line) => ui.text(line, { align: "center" })) : [])]);
      }
      const systems = SYSTEMS.filter((system) => state.games.some((game) => game.system === system.id));
      const items = phoneGames();
      return ui.column({ gap: 12 }, [
        systems.length > 1
          ? ui.choice({ id: "system", selected: state.filter, options: [{ id: "all", label: "All" }, ...systems.map((system) => ({ id: system.id, label: system.name }))] })
          : null,
        state.games.length > 8 ? ui.keyboard({ id: "search", mode: "field", placeholder: "Search games", submit_label: "Search", value: state.search }) : null,
        state.error ? ui.text(state.error, { size: "small", align: "center" }) : null,
        items.length ? ui.list({ id: "games", columns: 2, items }) : ui.text("No games match.", { align: "center" }),
      ]);
    }

    function playingLayout(ui, player) {
      const system = systemFor(state.current.system);
      const port = state.ports[player.id];
      const isHost = player.host;
      if (isHost && state.hostView === "load") {
        return ui.column({ gap: 12 }, [
          ui.text("Load a save", { size: "large", align: "center" }),
          state.loadList.length
            ? ui.list({ id: "load", columns: 1, items: state.loadList.map((entry) => ({ id: `${entry.scope}:${entry.key}`, title: since(entry.updatedAt), subtitle: entry.scope === "player" ? "Yours" : "This kiosk" })) })
            : ui.text("No saves for this game yet.", { align: "center" }),
          ui.button({ id: "back", label: "Back to the game", variant: "secondary" }),
        ]);
      }
      if (isHost && state.hostView === "pass") {
        const others = pad.players.filter((other) => other.connected && other.id !== player.id && other.slot);
        return ui.column({ gap: 12 }, [
          ui.text("Pass the controller to…", { size: "large", align: "center" }),
          others.length ? ui.list({ id: "pass", columns: 1, items: others.map((other) => ({ id: other.id, title: other.name, subtitle: state.ports[other.id] !== undefined ? `Player ${state.ports[other.id] + 1}` : "Watching" })) }) : ui.text("Nobody else has joined.", { align: "center" }),
          ui.button({ id: "back", label: "Back", variant: "secondary" }),
        ]);
      }
      if (isHost && state.hostView === "settings") {
        return ui.column({ gap: 12 }, [
          ui.text("Settings", { size: "large", align: "center" }),
          ui.button({ id: "crt", label: state.settings.crt ? "CRT look: on" : "CRT look: off", variant: "secondary" }),
          system.rewindOn ? ui.button({ id: "rewind", label: state.settings.rewind ? `Rewind (hold ${system.rewindOn === "L2" ? "ZL" : system.rewindOn}): on` : "Rewind: off", variant: "secondary" }) : null,
          ui.text("Changes apply the next time a game starts.", { size: "small", align: "center" }),
          ui.button({ id: "reset", label: confirmReset ? "Tap again to restart the game" : "Restart the game", variant: "danger" }),
          ui.button({ id: "back", label: "Back to the game", variant: "primary" }),
        ]);
      }
      const extras = isHost
        ? ui.row({ gap: 8 }, [
            system.ports < 2 || Object.keys(state.ports).length < 2 ? ui.button({ id: "pass_open", label: "Pass", variant: "ghost" }) : null,
            ui.button({ id: "settings", label: "Settings", variant: "ghost" }),
          ])
        : null;
      if (port === undefined) {
        return ui.column({ gap: 12 }, [
          extras,
          ui.text("Watching", { size: "large", align: "center" }),
          ui.text(`${state.current.systemName} has ${system.ports === 1 ? "one controller" : `${system.ports} controllers`}. Ask the host for a turn.`, { align: "center" }),
          ui.button({ id: "turn", label: "Ask for a turn", variant: "primary" }),
        ]);
      }
      const preset = ui.preset(phonePreset(system).name, { labels: phonePreset(system).labels, hide: phonePreset(system).hide });
      return extras ? ui.column({ gap: 6 }, [extras, preset]) : preset;
    }

    let confirmReset = false;

    pad.layout((player) => {
      const ui = pad.ui;
      if (state.phase === "loading") {
        const title = ui.text(`Loading ${state.current ? state.current.title : "the game"}…`, { size: "large", align: "center" });
        if (!state.loadingLabel) return title;
        return ui.column({ gap: 10 }, [title, ui.text(state.loadingLabel, { align: "center" }), player.host ? ui.button({ id: "cancel_load", label: "Cancel", variant: "secondary" }) : null]);
      }
      if (state.phase === "playing" && state.current) return playingLayout(ui, player);
      if (player.host) return hostLobby(ui);
      const host = pad.players.find((other) => other.host && other.connected);
      return ui.column({ gap: 12 }, [ui.text("Retro Arcade", { size: "large", align: "center" }), ui.text(host ? `${host.name} is choosing a game.` : "Waiting for the host.", { align: "center" })]);
    });

    pad.on("event", async ({ player, control, kind, value }) => {
      if (control === "turn" && kind === "tap") {
        const host = pad.players.find((other) => other.host && other.connected);
        if (host) pad.haptic(host, "tap").catch(() => {});
        toast(`${player.name} would like a turn`);
        return;
      }
      if (!player.host) return;
      if (state.phase === "loading" && control === "cancel_load" && kind === "tap") {
        quit();
        return;
      }
      if (state.phase === "lobby") {
        if (control === "games" && kind === "tap") {
          state.hover = String(value);
          sendView();
          pick(value);
        } else if (control === "system" && kind === "tap") {
          state.filter = String(value);
          sendView();
        } else if (control === "search" && kind === "submit") {
          state.search = String(value || "").slice(0, 60);
        } else if ((control === "continue" || control === "fresh") && kind === "tap" && pendingContinue) {
          const game = gameById(pendingContinue);
          pendingContinue = null;
          state.hostView = "games";
          if (game) start(game, { continue: control === "continue" });
        }
        return;
      }
      if (state.phase !== "playing" || kind !== "tap") return;
      switch (control) {
        case "settings":
          state.hostView = "settings";
          confirmReset = false;
          break;
        case "pass_open":
          state.hostView = "pass";
          break;
        case "back":
          state.hostView = "games";
          confirmReset = false;
          break;
        case "crt":
          state.settings = { ...state.settings, crt: !state.settings.crt };
          break;
        case "rewind":
          state.settings = { ...state.settings, rewind: !state.settings.rewind };
          break;
        case "reset":
          if (!confirmReset) {
            confirmReset = true;
            state.hostView = "settings";
            break;
          }
          confirmReset = false;
          state.hostView = "games";
          post({ type: "reset" });
          break;
        case "load": {
          const [scope, ...rest] = String(value).split(":");
          const data = await readBlob(rest.join(":"), scope);
          state.hostView = "games";
          if (data) {
            post({ type: "load_state", data });
            toast("Loaded");
          } else toast("Couldn't load that save");
          break;
        }
        case "pass": {
          // The chosen player takes port 1; whoever had it watches.
          const target = String(value);
          const ports = { ...state.ports };
          const previous = Object.keys(ports).find((id) => ports[id] === 0);
          const targetPort = ports[target];
          if (previous) delete ports[previous];
          ports[target] = 0;
          if (previous && targetPort !== undefined && targetPort !== 0) ports[previous] = targetPort;
          state.ports = ports;
          state.hostView = "games";
          post({ type: "ports", ports });
          pad.haptic(target, "success").catch(() => {});
          break;
        }
      }
    });

    // ---- the screen ----------------------------------------------------------------

    function sidebar() {
      const players = pad.players;
      return ctx.ui.column({ gap: 12, width: 230 }, [
        ctx.ui.h3("Scan to play"),
        pad.joinCard({ size: 180 }),
        ...(players.length
          ? players.map((player) => ctx.ui.row({ gap: 8, align: "center" }, [ctx.ui.text(player.name || "Player"), ctx.ui.small(player.host ? "Host" : player.connected ? "Joined" : "Reconnecting")]))
          : [ctx.ui.small("Nobody has joined yet.")]),
      ]);
    }

    refresh().then(loadHistory);

    // The page stays the first child so it never reloads when the sidebar comes and goes.
    return () =>
      ctx.ui.row({ gap: 18, align: "stretch" }, [
        ctx.ui.embed({ web: "index.html", channel: CHANNEL, input: true, title: "Retro Arcade", width: "100%", height: "calc(100vh - 150px)", minHeight: 520 }),
        ctx.ui.when(state.phase === "lobby" && Boolean(pad.session), sidebar()),
      ]);
  },
};

// <generated:systems>
// Retro Arcade's systems: cores, ports, phone labels and the input map.
// Used by the page (web/) and embedded into app.yui.js (bun run sync).
// RetroPad indices checked against EmulatorJS 4.2.3 (src/emulator.js
// defaultControllers, src/GameManager.js simulateInput).

const EMULATORJS_VERSION = "4.2.3";
const EMULATORJS_RUNTIME = "emulatorjs";

// RetroPad button indices (EmulatorJS simulateInput).
const RETROPAD = {
	B: 0, Y: 1, SELECT: 2, START: 3, UP: 4, DOWN: 5, LEFT: 6, RIGHT: 7,
	A: 8, X: 9, L: 10, R: 11, L2: 12, R2: 13, L3: 14, R3: 15,
	LX_POS: 16, LX_NEG: 17, LY_POS: 18, LY_NEG: 19,
	RX_POS: 20, RX_NEG: 21, RY_POS: 22, RY_NEG: 23,
	REWIND: 28,
};

// Yui input-frame bits (contracts §3) by name.
const PAD_BITS = {
	South: 0, East: 1, West: 2, North: 3, L1: 4, R1: 5, L2: 6, R2: 7,
	Select: 8, Start: 9, L3: 11, R3: 12, Up: 13, Down: 14, Left: 15, Right: 16,
};

// By position, like everywhere in Yui: South is the bottom face button.
const POSITION_TO_RETROPAD = {
	South: RETROPAD.B, East: RETROPAD.A, West: RETROPAD.Y, North: RETROPAD.X,
	L1: RETROPAD.L, R1: RETROPAD.R, L2: RETROPAD.L2, R2: RETROPAD.R2,
	Select: RETROPAD.SELECT, Start: RETROPAD.START, L3: RETROPAD.L3, R3: RETROPAD.R3,
	Up: RETROPAD.UP, Down: RETROPAD.DOWN, Left: RETROPAD.LEFT, Right: RETROPAD.RIGHT,
};

const ALL_BUTTONS = Object.keys(PAD_BITS);

/**
 * id: Yui's metadata.platform; core: EmulatorJS's EJS_core; cores: the
 * core files the runtime needs; ports: how many players at once; labels:
 * what the phone shows; buttons: which positions the system has (the rest
 * are hidden); analog: the left stick goes to the core as a stick (else it
 * drives the d-pad); rewindOn: the free button that holds rewind.
 */
const SYSTEMS = [
	{ id: "nes", name: "NES", core: "nes", cores: ["fceumm"], ports: 2, color: "#c8102e",
		labels: { South: "B", East: "A" }, buttons: ["South", "East", "Select", "Start"], rewindOn: "L2" },
	{ id: "snes", name: "Super NES", core: "snes", cores: ["snes9x"], ports: 2, color: "#6a5acd",
		labels: { South: "B", East: "A", West: "Y", North: "X", L1: "L", R1: "R" },
		buttons: ["South", "East", "West", "North", "L1", "R1", "Select", "Start"] },
	{ id: "gb", name: "Game Boy", core: "gb", cores: ["gambatte"], ports: 1, color: "#8b956d",
		labels: { South: "B", East: "A" }, buttons: ["South", "East", "Select", "Start"], rewindOn: "L2" },
	{ id: "gbc", name: "Game Boy Color", core: "gb", cores: ["gambatte"], ports: 1, color: "#7b3fa0",
		labels: { South: "B", East: "A" }, buttons: ["South", "East", "Select", "Start"], rewindOn: "L2" },
	{ id: "gba", name: "Game Boy Advance", core: "gba", cores: ["mgba"], ports: 1, color: "#4b3c9e",
		labels: { South: "B", East: "A", L1: "L", R1: "R" }, buttons: ["South", "East", "L1", "R1", "Select", "Start"] },
	{ id: "genesis", name: "Mega Drive", core: "segaMD", cores: ["genesis_plus_gx"], ports: 2, color: "#1f1f1f",
		labels: { West: "A", South: "B", East: "C", L1: "X", North: "Y", R1: "Z", Start: "Start", Select: "Mode" },
		buttons: ["West", "South", "East", "L1", "North", "R1", "Start", "Select"], rewindOn: "L2" },
	{ id: "sms", name: "Master System", core: "segaMS", cores: ["smsplus", "genesis_plus_gx"], ports: 2, color: "#d22630",
		labels: { South: "1", East: "2", Start: "Pause" }, buttons: ["South", "East", "Start"], rewindOn: "L2" },
	{ id: "gg", name: "Game Gear", core: "segaGG", cores: ["genesis_plus_gx"], ports: 1, color: "#2b2b2b",
		labels: { South: "1", East: "2", Start: "Start" }, buttons: ["South", "East", "Start"], rewindOn: "L2" },
	{ id: "pce", name: "PC Engine", core: "pce", cores: ["mednafen_pce"], ports: 1, color: "#e8e8e8",
		labels: { South: "II", East: "I", Select: "Select", Start: "Run" }, buttons: ["South", "East", "Select", "Start"], rewindOn: "L2" },
	{ id: "atari2600", name: "Atari 2600", core: "atari2600", cores: ["stella2014"], ports: 2, color: "#7a4a1d",
		labels: { South: "Fire", Select: "Select", Start: "Reset" }, buttons: ["South", "Select", "Start"], rewindOn: "L2" },
	{ id: "lynx", name: "Lynx", core: "lynx", cores: ["handy"], ports: 1, color: "#3a3a3a",
		labels: { South: "B", East: "A", L1: "Opt 1", R1: "Opt 2", Start: "Pause" }, buttons: ["South", "East", "L1", "R1", "Start"] },
];

const DPAD = ["Up", "Down", "Left", "Right"];

function systemFor(platform) {
	const id = String(platform ?? "").toLowerCase();
	return SYSTEMS.find((system) => system.id === id) ?? null;
}

/** The phone's gamepad preset for a system: labels and hidden buttons. */
function phonePreset(system) {
	const keep = new Set([...system.buttons, ...DPAD]);
	const hide = ALL_BUTTONS.filter((name) => !keep.has(name));
	if (!system.analog) hide.push("LeftStick");
	return { name: "gamepad", labels: { ...system.labels }, hide };
}

/**
 * Turns input frames into simulateInput calls. Keeps each port's last
 * state and returns only what changed: [[index, value], ...].
 */
function createInputMapper(system, options = {}) {
	const deadzone = options.deadzone ?? 0.5;
	const rewind = Boolean(options.rewind && system.rewindOn);
	let last = new Map();

	function wanted(frame) {
		const out = new Map();
		const buttons = Number(frame.buttons) >>> 0;
		const pressed = (name) => ((buttons >>> PAD_BITS[name]) & 1) === 1;
		for (const name of system.buttons) out.set(POSITION_TO_RETROPAD[name], pressed(name) ? 1 : 0);
		const lx = Number(frame.lx || 0) / 32767;
		const ly = Number(frame.ly || 0) / 32767; // +y is up
		if (system.analog) {
			out.set(RETROPAD.LX_POS, lx > 0 ? Math.round(lx * 0x7fff) : 0);
			out.set(RETROPAD.LX_NEG, lx < 0 ? Math.round(-lx * 0x7fff) : 0);
			out.set(RETROPAD.LY_POS, ly < 0 ? Math.round(-ly * 0x7fff) : 0); // RetroPad +y is down
			out.set(RETROPAD.LY_NEG, ly > 0 ? Math.round(ly * 0x7fff) : 0);
		}
		const stick = !system.analog;
		out.set(RETROPAD.UP, pressed("Up") || (stick && ly > deadzone) ? 1 : 0);
		out.set(RETROPAD.DOWN, pressed("Down") || (stick && ly < -deadzone) ? 1 : 0);
		out.set(RETROPAD.LEFT, pressed("Left") || (stick && lx < -deadzone) ? 1 : 0);
		out.set(RETROPAD.RIGHT, pressed("Right") || (stick && lx > deadzone) ? 1 : 0);
		if (rewind) out.set(RETROPAD.REWIND, pressed(system.rewindOn) ? 1 : 0);
		return out;
	}

	return {
		/** Changes for one frame. */
		update(frame) {
			const next = wanted(frame);
			const changes = [];
			for (const [index, value] of next) if ((last.get(index) ?? 0) !== value) changes.push([index, value]);
			last = next;
			return changes;
		},
		/** Releases everything held (port reassigned, pause, disconnect). */
		release() {
			const changes = [];
			for (const [index, value] of last) if (value !== 0) changes.push([index, 0]);
			last = new Map();
			return changes;
		},
	};
}

/** A stable hue from a title, for generated tiles. */
function titleHue(title) {
	let hash = 2166136261;
	for (const ch of String(title)) hash = Math.imul(hash ^ ch.charCodeAt(0), 16777619) >>> 0;
	return hash % 360;
}

const escapeXml = (text) => String(text).replace(/[<>&"']/g, (ch) => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;", "'": "&#39;" })[ch]);

/** A small SVG cartridge tile as a data URI (for phones; no canvas needed). */
function tileSvg(title, systemName) {
	const hue = titleHue(title);
	const words = String(title).toUpperCase().split(/\s+/).filter(Boolean);
	const lines = [];
	for (const word of words) {
		const lastLine = lines[lines.length - 1];
		if (lastLine && (lastLine + " " + word).length <= 12) lines[lines.length - 1] = lastLine + " " + word;
		else lines.push(word.slice(0, 14));
		if (lines.length === 3) break;
	}
	const text = lines
		.map((line, i) => `<text x="80" y="${78 + i * 20 - (lines.length - 1) * 10}" font-family="monospace" font-weight="700" font-size="15" fill="#fff" text-anchor="middle">${escapeXml(line)}</text>`)
		.join("");
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="160" height="160" viewBox="0 0 160 160"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="hsl(${hue} 70% 52%)"/><stop offset="1" stop-color="hsl(${(hue + 40) % 360} 65% 28%)"/></linearGradient></defs><rect width="160" height="160" rx="14" fill="url(#g)"/><rect x="10" y="10" width="140" height="18" rx="5" fill="rgba(0,0,0,.28)"/><text x="80" y="23" font-family="monospace" font-size="11" fill="#fff" text-anchor="middle">${escapeXml(String(systemName).toUpperCase())}</text>${text}</svg>`;
	return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

/** Save keys: `[a-z0-9][a-z0-9._-]{0,79}` (ctx.saves). */
function saveKeyBase(assetId) {
	return String(assetId).toLowerCase().replace(/[^a-z0-9._-]/g, "_").replace(/^[^a-z0-9]+/, "g").slice(0, 60);
}

function stateKey(assetId, n) {
	return `${saveKeyBase(assetId)}.state.${n}`;
}

function sramKey(assetId) {
	return `${saveKeyBase(assetId)}.sram`;
}

/** Newest save states first, from a ctx.saves.list() result. */
function statesFor(assetId, entries) {
	const prefix = `${saveKeyBase(assetId)}.state.`;
	return (entries || [])
		.filter((entry) => entry.key.startsWith(prefix))
		.sort((a, b) => String(b.updatedAt).localeCompare(String(a.updatedAt)));
}

/** The slot to write next: an empty one of 1-3, else the oldest. */
function nextStateSlot(assetId, entries) {
	const states = statesFor(assetId, entries);
	for (let n = 1; n <= 3; n++) if (!states.some((entry) => entry.key === stateKey(assetId, n))) return n;
	const oldest = states[states.length - 1];
	return Number(oldest.key.split(".").pop()) || 1;
}

/**
 * Ports for the players: the host first, then the others in seat order,
 * keeping anyone who already has a port. Returns { playerId: port (0-based) }.
 */
function assignPorts(players, system, current = {}) {
	const connected = players.filter((player) => player.connected !== false && player.slot);
	const ports = {};
	const taken = new Set();
	for (const player of connected) {
		const port = current[player.id];
		if (port !== undefined && port < system.ports && !taken.has(port)) {
			ports[player.id] = port;
			taken.add(port);
		}
	}
	const order = [...connected].sort((a, b) => Number(b.host) - Number(a.host) || (a.slot ?? 9) - (b.slot ?? 9));
	for (const player of order) {
		if (ports[player.id] !== undefined) continue;
		let port = 0;
		while (taken.has(port)) port++;
		if (port >= system.ports) break;
		ports[player.id] = port;
		taken.add(port);
	}
	return ports;
}

/**
 * Games out of ctx.assets("game"). Games that aren't on this kiosk yet are listed
 * too (available: false): "remote" ones download when picked (ctx.assets.request),
 * the rest are already on their way. `pending` counts those already downloading.
 */
function selectGames(assets) {
	const games = [];
	let pending = 0;
	for (const asset of Array.isArray(assets) ? assets : []) {
		const meta = asset?.metadata ?? {};
		const system = systemFor(meta.platform);
		if (!system) continue;
		const state = asset.state === undefined ? "ready" : String(asset.state);
		if (state !== "ready" && state !== "remote") pending += 1;
		const title = String(asset.title || meta.title || "Untitled game");
		const cover = typeof meta.cover_url === "string" && /^(https:\/\/|http:\/\/(localhost|127\.0\.0\.1)[:/])/.test(meta.cover_url) ? meta.cover_url : "";
		const size = Number(asset.byte_size) || 0;
		games.push({
			id: String(asset.id), title, system: system.id, systemName: system.name, cover,
			fromLink: Boolean(meta.source && meta.source.provider),
			state, available: state === "ready",
			progress: state === "ready" ? 1 : size > 0 ? Math.min(1, (Number(asset.bytes_done) || 0) / size) : 0,
			error: typeof asset.error === "string" ? asset.error : "",
		});
	}
	games.sort((a, b) => a.title.localeCompare(b.title) || a.id.localeCompare(b.id));
	return { games, pending };
}

/** What a tile or the loading screen says about a game that isn't here yet. */
function fetchLabel(game) {
	if (!game || game.available) return "";
	if (game.state === "remote") return game.error ? "Couldn't download" : "Downloads when picked";
	if (game.state === "fetching") return "Getting it from the server…";
	if (game.state === "failed") return "Download failed, retrying";
	const pct = Math.round((game.progress || 0) * 100);
	return pct > 0 ? `Downloading ${pct}%` : "Downloading…";
}

/** Library title ids match [A-Za-z0-9][A-Za-z0-9._:-]{0,127}. */
function libraryIdFor(assetId) {
	return `game-${String(assetId).replace(/[^A-Za-z0-9._:-]/g, "_")}`.slice(0, 128);
}
// </generated:systems>
