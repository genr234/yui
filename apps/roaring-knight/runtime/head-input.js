(() => {
  const canvas = document.getElementById("canvas");
  const run = new URLSearchParams(location.search).get("yui-run");
  if (!window.yui || !canvas || !run) return;

  const keys = { up: "t", down: "g", left: "f", right: "h" };
  const held = new Set();
  let watchdog;
  let paused = false;

  function setPaused(wanted) {
    if (wanted === paused || !canvas.classList.contains("active")) return;
    if (typeof GM_pause !== "function" || typeof GM_unpause !== "function") return;
    if (wanted) GM_pause();
    else GM_unpause();
    paused = wanted;
  }

  function update(directions) {
    for (const [direction, key] of Object.entries(keys)) {
      const pressed = directions[direction] === true;
      if (pressed === held.has(direction)) continue;
      if (pressed) held.add(direction);
      else held.delete(direction);
      const letter = key.toUpperCase();
      canvas.dispatchEvent(new KeyboardEvent(pressed ? "keydown" : "keyup", {
        key,
        code: `Key${letter}`,
        keyCode: letter.charCodeAt(0),
        bubbles: true,
        cancelable: true,
      }));
    }
  }

  function clear() {
    clearTimeout(watchdog);
    update({});
  }

  window.yui.on((data) => {
    if (data?.type !== "head-input" || data.run !== run || document.hidden) return;
    clearTimeout(watchdog);
    setPaused(data.paused === true);
    update(paused ? {} : data.directions || {});
    watchdog = setTimeout(clear, 250);
  });
  window.addEventListener("keydown", (event) => {
    if (event.code !== "KeyR" || event.repeat) return;
    event.preventDefault();
    window.yui.post({ type: "head-recenter", run });
  });
  window.addEventListener("pagehide", clear);
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) clear();
  });
  window.yui.ready.then(() => window.yui.post({ type: "head-input-ready", run }));
})();
