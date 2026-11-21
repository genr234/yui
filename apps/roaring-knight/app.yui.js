export default {
  schema: "yui.simple-js.v0",
  id: "dev.genr.roaring-knight",
  name: "Deltarune Boss Practice",
  version: "0.7.1",
  category: "Games",
  description: "Start a Deltarune boss fight directly.",
  permissions: ["assets:runtime", "fullscreen", "controllers", "camera"],
  web: { runtimes: ["roaring-knight"] },

  controllers: {
    keymap: {
      players: "host",
      controls: [
        { type: "dpad", keys: { up: "i", down: "k", left: "j", right: "l" } },
        { type: "button", label: "Confirm", key: "z" },
        { type: "button", label: "Cancel", key: "x" },
        { type: "button", label: "Menu", key: "c" },
        { type: "button", label: "Recenter / Resume", key: "r" },
      ],
    },
  },

  mount(ctx) {
    const state = ctx.state({
      ready: false, error: "", fight: "", head: "off", run: 0,
      rehearsal: false, paused: false, sensitivity: "Medium",
      // Setup feedback: is a face in view, and how much of the hold is done (0–100).
      seen: false, hold: 0,
    });
    let camera;
    let subscribed = false;
    let frameReady = false;
    let neutral;
    let samples = [];
    let previous;
    let lastFaceAt = 0;
    let pilotReady = false;
    let smoothed = { x: 0, y: 0 };
    let directions = {};

    ctx.embeds.on("battle", (message) => {
      if ((!state.fight && !state.rehearsal) || message?.run !== String(state.run)) return;
      if (message.type === "head-input-ready") {
        frameReady = true;
        send(directions);
      } else if (message.type === "head-recenter" && camera) {
        recover();
      }
    });

    function send(next) {
      if (frameReady && (state.fight || state.rehearsal)) {
        ctx.embeds.post("battle", {
          type: "head-input", run: String(state.run), directions: state.paused ? {} : next, paused: state.paused,
        });
      }
    }

    function pauseForRecovery() {
      if (state.fight && !["giga-queen", "eram"].includes(state.fight) && pilotReady) state.paused = true;
    }

    function release() {
      smoothed = { x: 0, y: 0 };
      directions = {};
      send(directions);
    }

    function recenter() {
      pauseForRecovery();
      neutral = undefined;
      samples = [];
      setSetup(false, 0);
      previous = undefined;
      state.head = "setup";
      release();
    }

    function recover() {
      if (state.paused && state.head === "on") {
        state.paused = false;
        release();
      } else {
        recenter();
      }
    }

    // Only re-render when the setup feedback visibly changes.
    function setSetup(seen, hold) {
      if (state.seen !== seen) state.seen = seen;
      if (state.hold !== hold) state.hold = hold;
    }

    function onFace(face) {
      if (!camera || state.head === "recenter") return;
      const now = Date.now();
      if (!face) {
        if (neutral && now - lastFaceAt < 110) return;
        samples = [];
        if (!neutral) setSetup(false, 0);
        if (neutral && state.head !== "lost") state.head = "lost";
        if (neutral && now - lastFaceAt >= 450) pauseForRecovery();
        release();
        return;
      }

      if (!neutral) {
        if (samples.length && faceDistance(face, samples[0]) > SETUP_WOBBLE) samples = [];
        samples.push(face);
        setSetup(true, Math.round((samples.length / SETUP_SAMPLES) * 10) * 10);
        if (samples.length < SETUP_SAMPLES) return;
        neutral = averageFace(samples);
        pilotReady = true;
      } else if ((previous && faceDistance(face, previous) > 0.65)
        || (state.head === "lost" && now - lastFaceAt > 800)) {
        pauseForRecovery();
        state.head = "recenter";
        release();
        return;
      }

      previous = face;
      lastFaceAt = now;
      if (state.head !== "on") state.head = "on";
      // Mirror horizontal steering so it follows the player's mirrored preview.
      const x = (neutral.x - face.x) / neutral.width;
      const y = (face.y - neutral.y) / neutral.height;
      smoothed = { x: smooth(x, smoothed.x), y: smooth(y, smoothed.y) };
      const threshold = sensitivity[state.sensitivity];
      directions = {
        left: steer(-smoothed.x, directions.left, threshold),
        right: steer(smoothed.x, directions.right, threshold),
        up: steer(-smoothed.y, directions.up, threshold),
        down: steer(smoothed.y, directions.down, threshold),
      };
      send(directions);
    }

    async function enableCamera() {
      if (camera || state.head === "opening") return;
      state.head = "opening";
      try {
        camera = await ctx.camera.open({ width: 640, height: 480, facing: "user" });
        if (!subscribed) {
          ctx.camera.on("face", onFace);
          ctx.camera.on("state", ({ state: status }) => {
            if (status === "open" || !camera) return;
            pauseForRecovery();
            camera = undefined;
            state.head = "unavailable";
            release();
          });
          subscribed = true;
        }
        if (!state.fight) openRehearsal();
        recenter();
      } catch {
        await camera?.stop();
        camera = undefined;
        state.head = "unavailable";
      }
    }

    async function stopCamera() {
      const handle = camera;
      camera = undefined;
      state.head = "off";
      state.paused = false;
      pilotReady = false;
      release();
      await handle?.stop();
    }

    function openRehearsal() {
      frameReady = false;
      state.run += 1;
      state.rehearsal = true;
    }

    function headMessage() {
      if (state.head !== "setup") return headMessages[state.head];
      return state.seen ? `Face found. Hold still… ${state.hold}%` : headMessages.searching;
    }

    function headControls() {
      const setup = state.head === "setup" || state.head === "recenter";
      return ctx.ui.column({ gap: 8 }, [
        ctx.ui.row({ gap: 8, align: "center", wrap: true }, [
          ...(!camera && state.head !== "opening" ? [
            ctx.ui.button({ label: "Enable head control", onClick: enableCamera }),
          ] : []),
          ...(camera ? [
            ctx.ui.button({ label: "Recenter", onClick: recenter }),
            ctx.ui.button({ label: "Turn off camera", onClick: stopCamera }),
          ] : state.paused ? [
            ctx.ui.button({ label: "Continue with keyboard", onClick: stopCamera }),
          ] : []),
          ...(camera && state.head === "on" && !state.fight && !state.rehearsal ? [
            ctx.ui.button({ label: "Practice steering", onClick: openRehearsal }),
          ] : []),
          ...(state.paused ? [ctx.ui.text("Fight paused")] : []),
          ...(headMessage() ? [ctx.ui.text(headMessage())] : []),
        ]),
        ...(state.rehearsal ? [ctx.ui.row({ gap: 8, align: "center", wrap: true }, [
          ctx.ui.text(`Sensitivity · ${state.sensitivity}`),
          ...Object.keys(sensitivity).map((label) => ctx.ui.button({ label, onClick: () => {
            state.sensitivity = label;
            release();
          } })),
        ])] : []),
        // The preview boxes the face it follows, so the player sees the camera has them.
        ...(setup || state.head === "lost" ? [ctx.ui.camera({ mirror: true, width: 240, fit: "contain" })] : []),
        ...(camera && state.paused && state.head === "on" ? [
          ctx.ui.row({ gap: 8 }, [ctx.ui.button({ label: "Resume fight", onClick: recover })]),
        ] : []),
      ]);
    }

    async function load() {
      state.error = "";
      try {
        await ctx.web.runtime("roaring-knight");
        state.ready = true;
      } catch (error) {
        state.error = `Could not open the local game runtime. ${error.message}`;
      }
    }

    void load();
    return () => {
      if (!state.ready) {
        return ctx.ui.column({ gap: 12 }, [
          ctx.ui.text(state.error || "Opening the local game…"),
          ...(state.error ? [ctx.ui.button({ label: "Retry", onClick: load })] : []),
        ]);
      }

      const fight = fights.find(({ id }) => id === state.fight);
      if (state.rehearsal) {
        return ctx.ui.column({ gap: 12, grow: true }, [
          ctx.ui.button({ label: "Choose a fight", onClick: () => {
            release();
            frameReady = false;
            state.rehearsal = false;
          } }),
          headControls(),
          ctx.ui.embed({
            web: `head-rehearsal.html?run=${state.run}`, channel: "battle", title: "Practice steering",
            fill: true, aspect: "4:3",
          }),
        ]);
      }
      if (!fight) {
        return ctx.ui.column({ gap: 12 }, [
          headControls(),
          ctx.ui.h1("Choose a fight"),
          ctx.ui.row({ gap: 24, align: "start", wrap: true }, chapters.map(({ label, fights }) =>
            ctx.ui.column({ gap: 8, width: 200 }, [
              ctx.ui.text(label),
              ...fights.map(({ id, name }) => ctx.ui.button({
                label: name,
                onClick: () => {
                  frameReady = false;
                  state.run += 1;
                  state.fight = id;
                },
              })),
            ]),
          )),
        ]);
      }

      return ctx.ui.column({ gap: 8, grow: true }, [
        ctx.ui.button({ label: "Switch fight", onClick: () => {
          release();
          frameReady = false;
          state.paused = false;
          state.fight = "";
        } }),
        headControls(),
        ctx.ui.embed({
          web: `index.html?fight=${fight.id}&run=${state.run}`,
          channel: "battle",
          title: fight.name,
          fill: true,
          aspect: "4:3",
        }),
      ]);
    };
  },
};

const chapters = [
  { label: "Chapter 1", fights: [{ id: "king", name: "King" }, { id: "jevil", name: "Jevil" }] },
  { label: "Chapter 2", fights: [
    { id: "queen", name: "Queen" },
    { id: "giga-queen", name: "Giga Queen" },
    { id: "spamton", name: "Spamton" },
    { id: "spamton-neo", name: "Spamton NEO" },
  ] },
  { label: "Chapter 3", fights: [
    { id: "tenna", name: "Tenna" },
    { id: "knight", name: "Roaring Knight" },
    { id: "eram", name: "ERAM" },
  ] },
  { label: "Chapter 4", fights: [
    { id: "jackenstein", name: "Jackenstein" },
    { id: "sound-of-justice", name: "Sound of Justice" },
    { id: "titan", name: "Titan" },
    { id: "gerson", name: "Hammer of Justice (Gerson)" },
  ] },
];
const fights = chapters.flatMap((chapter) => chapter.fights);

const headMessages = {
  opening: "Opening the kiosk camera…",
  setup: "Face the camera and hold still to center your steering.",
  searching: "Looking for your face… Stand facing the camera, one or two arm's lengths away, with your face in the light.",
  on: "Head control on. Lean gently to steer the heart.",
  lost: "Face out of view. Look toward the camera.",
  recenter: "Position changed. Recenter to restore steering.",
  unavailable: "Camera unavailable. You can still use the keyboard arrows.",
};

function faceDistance(first, second) {
  return Math.max(
    Math.abs(first.x - second.x) / second.width,
    Math.abs(first.y - second.y) / second.height,
    Math.abs(first.width - second.width) / second.width,
  );
}

function averageFace(samples) {
  const total = { x: 0, y: 0, width: 0, height: 0 };
  for (const sample of samples) {
    for (const key of Object.keys(total)) total[key] += sample[key];
  }
  for (const key of Object.keys(total)) total[key] /= samples.length;
  return total;
}

const sensitivity = { Low: 0.23, Medium: 0.16, High: 0.11 };
/** Steady readings needed to set the center, and how far (in face sizes) they may wander. */
const SETUP_SAMPLES = 10;
const SETUP_WOBBLE = 0.2;

function smooth(target, previous) {
  const weight = Math.abs(target) < Math.abs(previous) || target * previous < 0 ? 0.7 : 0.5;
  return weight * target + (1 - weight) * previous;
}

function steer(displacement, held, threshold) {
  return displacement > threshold * (held ? 0.55 : 1);
}
