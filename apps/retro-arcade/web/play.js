// Runs one game in this frame (index.html makes a new frame per game, so
// EmulatorJS always starts clean). index.html calls window.boot(config) and
// reads window.arcadeEvents.
(function () {
  "use strict";
  const listeners = new Set();
  window.arcadeEvents = {
    on(handler) {
      listeners.add(handler);
      return () => listeners.delete(handler);
    },
  };
  const emit = (event) => {
    for (const handler of listeners) {
      try {
        handler(event);
      } catch (error) {
        console.error(error);
      }
    }
  };

  window.boot = function boot(config) {
    window.EJS_player = "#game";
    window.EJS_core = config.core;
    window.EJS_gameUrl = config.gameUrl;
    window.EJS_gameName = config.gameName || "game";
    window.EJS_pathtodata = config.runtimeUrl;
    window.EJS_startOnLoaded = true;
    window.EJS_threads = false;
    window.EJS_language = "en-US";
    window.EJS_disableAutoLang = false; // EmulatorJS reads `false` as "don't guess"
    window.EJS_disableDatabases = true; // saves go through ctx.saves, not IndexedDB
    window.EJS_noAutoFocus = false;
    window.EJS_volume = 1;
    window.EJS_backgroundColor = "#000";
    window.EJS_color = "#000";
    // No EmulatorJS buttons or menus: the phone's Home sheet does all of that.
    window.EJS_Buttons = {
      playPause: false, restart: false, mute: false, settings: false, fullscreen: false,
      saveState: false, loadState: false, screenRecord: false, gamepad: false, cheat: false,
      volume: false, saveSavFiles: false, loadSavFiles: false, quickSave: false, quickLoad: false,
      screenshot: false, cacheManager: false, exitEmulation: false, netplay: false, diskButton: false,
      rightClick: false,
    };
    // Keyboard and real gamepads stay unmapped: input comes from the phones.
    window.EJS_defaultControls = { 0: {}, 1: {}, 2: {}, 3: {} };
    window.EJS_defaultOptions = Object.assign(
      { "save-state-location": "browser", rewindEnabled: config.rewind ? "enabled" : "disabled" },
      config.crt ? { shader: "crt-easymode.glslp" } : {},
    );
    if (config.crt) document.body.classList.add("smooth");

    window.EJS_ready = () => emit({ type: "progress", label: "Starting…" });
    window.EJS_onGameStart = () => {
      const manager = window.EJS_emulator && window.EJS_emulator.gameManager;
      if (manager && config.sram) {
        try {
          const path = manager.getSaveFilePath();
          const dir = path.slice(0, path.lastIndexOf("/"));
          try { manager.FS.mkdirTree(dir); } catch (_) {}
          manager.FS.writeFile(path, config.sram);
          manager.loadSaveFiles();
        } catch (error) {
          console.warn("[retro-arcade] could not restore the save file", error);
        }
      }
      if (manager && config.state) {
        try {
          manager.loadState(config.state);
        } catch (error) {
          console.warn("[retro-arcade] could not load the state", error);
        }
      }
      emit({ type: "started" });
    };

    const script = document.createElement("script");
    script.src = config.runtimeUrl + "loader.js";
    script.onerror = () => emit({ type: "error", message: "The emulator files did not load." });
    document.body.appendChild(script);
    emit({ type: "progress", label: "Loading the emulator…" });

    // EmulatorJS reports fatal problems as text in its own (hidden) UI.
    const watch = window.setInterval(() => {
      const text = document.querySelector(".ejs_error_text");
      if (text && text.textContent.trim()) {
        window.clearInterval(watch);
        emit({ type: "error", message: text.textContent.trim() });
      }
      if (window.EJS_emulator && window.EJS_emulator.started) window.clearInterval(watch);
    }, 500);
  };

  // Helpers index.html calls once the game runs.
  window.arcade = {
    manager: () => window.EJS_emulator && window.EJS_emulator.gameManager,
    input(port, index, value) {
      const manager = this.manager();
      if (manager) manager.simulateInput(port, index, value);
    },
    pause(paused) {
      const emulator = window.EJS_emulator;
      if (!emulator || !emulator.started) return;
      if (paused && !emulator.paused) emulator.pause();
      if (!paused && emulator.paused) emulator.play();
    },
    state() {
      const manager = this.manager();
      return manager ? manager.getState() : null;
    },
    loadState(data) {
      const manager = this.manager();
      if (manager) manager.loadState(data);
    },
    sram() {
      const manager = this.manager();
      if (!manager) return null;
      try {
        manager.saveSaveFiles();
        const path = manager.getSaveFilePath();
        return manager.FS.analyzePath(path).exists ? manager.FS.readFile(path) : null;
      } catch (_) {
        return null;
      }
    },
    reset() {
      const manager = this.manager();
      if (manager) manager.restart();
    },
  };
})();
