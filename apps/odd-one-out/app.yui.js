export default {
  schema: "yui.simple-js.v0",
  id: "dev.genr.odd-one-out",
  name: "L'Intruso",
  version: "0.1.0",
  icon: "🖍️",
  category: "Games",
  description: "Ognuno disegna un tratto di una parola segreta. Uno di voi non la conosce: riuscite a scoprirlo?",
  permissions: ["controllers", "library"],

  // Declaring controllers gives this app the phones while it is open.
  controllers: {},

  mount(ctx) {
    const pad = ctx.controllers;

    // Short, family friendly, easy to sketch.
    const categories = {
      Animali: ["gatto", "pesce", "elefante", "lumaca", "gufo", "giraffa", "farfalla", "pinguino", "tartaruga", "coniglio", "ragno", "papera"],
      Cibo: ["pizza", "banana", "gelato", "carota", "spaghetti", "anguria", "uovo", "formaggio", "mela", "panino", "biscotto", "ciambella"],
      Luoghi: ["castello", "faro", "piramide", "igloo", "fattoria", "scuola", "mulino a vento", "casa sull'albero", "spiaggia", "ponte", "tenda", "vulcano"],
      Trasporti: ["bicicletta", "barca a vela", "elicottero", "treno", "trattore", "razzo", "autobus", "mongolfiera", "monopattino", "camion dei pompieri", "sottomarino", "motorino"],
      "In casa": ["lampada", "ombrello", "orologio", "forbici", "spazzolino", "chiave", "caffettiera", "scala", "candela", "sedia", "letto", "specchio"],
      Natura: ["arcobaleno", "montagna", "cactus", "pupazzo di neve", "sole", "tornado", "fungo", "cascata", "foglia", "luna", "fiore", "nuvola"],
      "Hobby e sport": ["chitarra", "aquilone", "scacchi", "pesca", "campeggio", "pittura", "bowling", "calcio", "yo-yo", "telescopio", "tamburo", "puzzle"],
      Vestiti: ["calzino", "cappello", "guanto", "scarpa da ginnastica", "occhiali", "sciarpa", "zaino", "corona", "cravatta", "stivale", "vestito", "orologio da polso"],
      Mestieri: ["pompiere", "cuoco", "astronauta", "contadino", "pirata", "dottore", "clown", "muratore", "mago", "pittore", "postino", "giardiniere"],
    };
    const categoryNames = Object.keys(categories);

    const LAPS = 2; // strokes each, around the table
    const ROUNDS = 3;
    const MIN_PLAYERS = 3;
    const MAX_PLAYERS = 8;
    const TURN_MS = 30_000;
    const VOTE_MS = 45_000;
    const GUESS_MS = 30_000;
    const SIZES = [4, 8, 14]; // brush px at 360 px width, like the phone
    const MAX_POINTS = 2500; // per stroke
    const MAX_BATCH = 64; // per change event
    const W = 360; // drawing space: 4:3, brush sizes are in these units
    const H = 270;
    const palette = ["#e63946", "#1d7de0", "#2a9d4a", "#f0a90a", "#8e44c9", "#e8590c", "#0f9d9a", "#d6336c"];

    const state = ctx.state({
      phase: "lobby", // lobby | draw | vote | guess | reveal | done
      round: 0,
      scores: {}, // player id → points
      roster: [], // player ids in this round
      order: [], // roster in turn order
      step: 0, // which turn of order.length * LAPS
      names: {},
      colors: {},
      category: "",
      word: "",
      options: [], // the word and its decoys, for the guess
      odd: "", // the odd one out
      votes: {}, // voter id → suspect id
      guess: null,
      result: null, // { tally, caught, guessRight, gains }
      aborted: "", // why the round ended early
      rev: 0, // bumps when the drawing changes
      deadline: 0,
      clock: 0,
    });

    // The shared drawing lives outside ctx.state: it changes 30 times a second
    // and only the kiosk view reads it, so rev tells the view when to redraw.
    let strokes = []; // { color, size, points: [[x, y], …] }
    let cur = { stroke: null, shape: null }; // the one stroke this turn
    const used = new Set();

    ctx.library
      .publish([{ id: "classic", title: "L'Intruso", players: MAX_PLAYERS, progress: 0 }])
      .catch(() => {});

    // Haptics are a nicety: never wait on them or let them fail a round.
    const buzz = (who, name) => pad.haptic(who, name).catch(() => {});

    // ---- small helpers ----
    const pick = (list) => list[Math.floor(Math.random() * list.length)];
    function shuffled(list) {
      const out = [...list];
      for (let i = out.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [out[i], out[j]] = [out[j], out[i]];
      }
      return out;
    }
    const pts = (n) => `${n} ${n === 1 ? "punto" : "punti"}`;
    const voted = (n, of) => `${n} su ${of} ${n === 1 ? "ha" : "hanno"} votato`;
    const here = (id) => pad.players.some((p) => p.id === id && p.connected !== false);
    const nameOf = (id) => state.names[id] || pad.players.find((p) => p.id === id)?.name || "Giocatore";
    const present = () => state.roster.filter(here);
    const total = () => state.order.length * LAPS;
    const current = () => state.order[state.step % state.order.length];
    const sketchId = () => `r${state.round}-t${state.step}`; // a new id each turn: a blank pad

    // ---- one timer for whatever the room is waiting on ----
    let timer = null;
    let ticker = null;
    function disarm() {
      if (timer) clearTimeout(timer);
      if (ticker) clearInterval(ticker);
      timer = ticker = null;
      state.deadline = 0;
    }
    function arm(ms, fn) {
      disarm();
      state.clock = Date.now();
      state.deadline = state.clock + ms;
      timer = setTimeout(fn, ms);
      ticker = setInterval(() => (state.clock = Date.now()), 1000);
    }

    // ---- the drawing ----
    let revHold = null;
    let revPending = false;
    function bumpRev() {
      if (revHold) {
        revPending = true;
        return;
      }
      state.rev += 1;
      revHold = setTimeout(() => {
        revHold = null;
        if (revPending) {
          revPending = false;
          bumpRev();
        }
      }, 60);
    }

    const goodColor = (c) => typeof c === "string" && (/^#[0-9a-f]{3,8}$/i.test(c) || /^[a-z]{3,20}$/i.test(c));
    // Players carry a colour name; these are the platform's swatches for them,
    // which read on a white canvas (plain CSS "yellow" would not).
    const swatches = { blue: "#2f76dc", red: "#d84a4a", green: "#1f9d68", yellow: "#e2a60f", purple: "#6558d8", orange: "#e5761f", pink: "#d64f95", teal: "#0f9f8f" };
    function colorFor(player, index) {
      if (swatches[player.color]) return swatches[player.color];
      return /^#[0-9a-f]{3,8}$/i.test(player.color ?? "") ? player.color : palette[index % palette.length];
    }
    const num = (v) => (Number.isFinite(v) ? Math.min(1, Math.max(0, v)) : null);
    const r1 = (v) => Math.round(v * 10) / 10;

    // One stroke as an SVG path: Catmull-Rom through the points, as on the phone.
    function strokePath(s) {
      const pts = s.points.map((p) => [p[0] * W, p[1] * H]);
      if (pts.length === 1) {
        return `<circle cx="${r1(pts[0][0])}" cy="${r1(pts[0][1])}" r="${s.size / 2}" fill="${s.color}"/>`;
      }
      let d = `M${r1(pts[0][0])} ${r1(pts[0][1])}`;
      for (let i = 0; i < pts.length - 1; i++) {
        const p0 = pts[Math.max(0, i - 1)];
        const p1 = pts[i];
        const p2 = pts[i + 1];
        const p3 = pts[Math.min(pts.length - 1, i + 2)];
        const c1 = [p1[0] + (p2[0] - p0[0]) / 6, p1[1] + (p2[1] - p0[1]) / 6];
        const c2 = [p2[0] - (p3[0] - p1[0]) / 6, p2[1] - (p3[1] - p1[1]) / 6];
        d += `C${r1(c1[0])} ${r1(c1[1])} ${r1(c2[0])} ${r1(c2[1])} ${r1(p2[0])} ${r1(p2[1])}`;
      }
      return `<path d="${d}" fill="none" stroke="${s.color}" stroke-width="${s.size}" stroke-linecap="round" stroke-linejoin="round"/>`;
    }
    const dataUrl = (svg) => `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
    function drawingUrl() {
      const body = strokes.map(strokePath).join("");
      return dataUrl(`<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}"><rect width="${W}" height="${H}" fill="#fff"/>${body}</svg>`);
    }
    const swatch = (color) =>
      ctx.ui.image({
        src: dataUrl(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"><circle cx="8" cy="8" r="7" fill="${goodColor(color) ? color : "#888"}"/></svg>`),
        alt: "",
        width: 16,
        height: 16,
      });
    let cache = { rev: -1, url: "" };
    function drawing() {
      if (cache.rev !== state.rev) cache = { rev: state.rev, url: drawingUrl() };
      return ctx.ui.image({ src: cache.url, alt: "Il disegno di tutti", width: "min(100%, 80vh)" });
    }

    // ---- round flow ----
    function beginRound() {
      const players = pad.players.filter((p) => p.connected !== false).slice(0, MAX_PLAYERS);
      if (players.length < MIN_PLAYERS) {
        state.phase = "lobby";
        return;
      }
      const roster = players.map((p) => p.id);
      const start = state.round % roster.length; // the starting player rotates
      const category = pick(categoryNames);
      const words = categories[category];
      const fresh = words.filter((w) => !used.has(w));
      const word = pick(fresh.length ? fresh : words);
      used.add(word);
      strokes = [];
      cur = { stroke: null, shape: null };
      const colors = {};
      players.forEach((p, i) => (colors[p.id] = colorFor(p, i)));
      state.names = Object.fromEntries(players.map((p) => [p.id, p.name || "Giocatore"]));
      state.colors = colors;
      state.roster = roster;
      state.order = [...roster.slice(start), ...roster.slice(0, start)];
      state.category = category;
      state.word = word;
      state.options = shuffled([word, ...shuffled(words.filter((w) => w !== word)).slice(0, 3)]);
      state.odd = pick(roster);
      state.votes = {};
      state.guess = null;
      state.result = null;
      state.aborted = "";
      state.step = -1;
      bumpRev();
      advance();
    }

    // Next turn: skip anyone who has left; after the last lap, vote.
    function advance() {
      do {
        state.step += 1;
      } while (state.step < total() && !here(current()));
      if (state.step >= total()) {
        startVote();
        return;
      }
      state.phase = "draw";
      cur = { stroke: null, shape: null };
      arm(TURN_MS, advance);
      buzz(current(), "hit"); // your turn
    }

    function startVote() {
      state.phase = "vote";
      state.votes = {};
      arm(VOTE_MS, finishVote);
      buzz("all", "countdown");
    }

    function voteDone() {
      return present().every((id) => state.votes[id] !== undefined);
    }

    function finishVote() {
      disarm();
      const tally = {};
      for (const target of Object.values(state.votes)) tally[target] = (tally[target] ?? 0) + 1;
      const top = Math.max(0, ...Object.values(tally));
      const leaders = Object.keys(tally).filter((id) => tally[id] === top);
      // A tie for the most votes means nobody was caught.
      const caught = top > 0 && leaders.length === 1 && leaders[0] === state.odd;
      state.result = { tally, caught, guessRight: false, gains: {} };
      if (caught) {
        state.phase = "guess";
        arm(GUESS_MS, () => resolveGuess(null));
        buzz(state.odd, "fail");
        return;
      }
      buzz(state.odd, "success");
      for (const id of present()) if (id !== state.odd) buzz(id, "fail");
      finishRound({ [state.odd]: 2 });
    }

    function resolveGuess(choice) {
      disarm();
      const right = choice !== null && choice === state.word;
      state.guess = choice;
      state.result = { ...state.result, guessRight: right };
      const gains = {};
      if (right) gains[state.odd] = 1;
      else for (const id of present()) if (id !== state.odd) gains[id] = 1;
      buzz(state.odd, right ? "success" : "fail");
      finishRound(gains);
    }

    function finishRound(gains) {
      const scores = { ...state.scores };
      for (const [id, n] of Object.entries(gains)) scores[id] = (scores[id] ?? 0) + n;
      state.scores = scores;
      state.result = { ...state.result, gains };
      state.phase = "reveal";
    }

    function abortRound(why) {
      disarm();
      state.aborted = why;
      state.result = null;
      state.phase = "reveal";
    }

    const isLast = () => state.round + 1 >= ROUNDS || pad.players.filter((p) => p.connected !== false).length < MIN_PLAYERS;

    function next() {
      if (isLast()) {
        state.phase = "done";
        const top = Math.max(0, ...pad.players.map((p) => state.scores[p.id] ?? 0));
        for (const p of pad.players) buzz(p.id, top > 0 && (state.scores[p.id] ?? 0) === top ? "win" : "tap");
        ctx.library.played("classic", { progress: 1 }).catch(() => {});
        return;
      }
      state.round += 1;
      ctx.library.played("classic", { progress: state.round / ROUNDS }).catch(() => {});
      beginRound();
    }

    function restart() {
      state.round = 0;
      state.scores = {};
      used.clear();
      beginRound();
    }

    // ---- the drawing pad's events ----
    // A turn takes the first stroke number the player sends and nothing else:
    // other numbers, undo and clear are ignored, and `submit` ends the turn.
    function onSketch(player, kind, value) {
      if (state.phase !== "draw" || player.id !== current()) return;
      const n = Number(value?.stroke);
      if (!Number.isInteger(n) || n < 1) return;
      if (kind === "change") {
        if (cur.stroke === null) {
          const size = SIZES.includes(value.size) ? value.size : SIZES[1];
          cur = { stroke: n, shape: { color: state.colors[player.id], size, points: [] } };
          strokes = [...strokes, cur.shape]; // always in the player's own colour
        } else if (n !== cur.stroke) return;
        if (!Array.isArray(value.points)) return;
        for (const p of value.points.slice(0, MAX_BATCH)) {
          if (cur.shape.points.length >= MAX_POINTS) break;
          const x = Array.isArray(p) ? num(p[0]) : null;
          const y = Array.isArray(p) ? num(p[1]) : null;
          if (x !== null && y !== null) cur.shape.points.push([x, y]);
        }
        if (cur.shape.points.length) bumpRev();
      } else if (kind === "submit" && n === cur.stroke && cur.shape.points.length) {
        bumpRev();
        advance();
      }
    }

    pad.on("event", ({ player, control, kind, value }) => {
      if (state.phase === "draw" && control === sketchId()) return onSketch(player, kind, value);
      if (kind !== "tap") return;
      const inRound = state.roster.includes(player.id) && here(player.id);
      if (control === "vote" && state.phase === "vote" && inRound) {
        if (state.votes[player.id] !== undefined || player.id === value || !state.roster.includes(value)) return;
        state.votes = { ...state.votes, [player.id]: value };
        if (voteDone()) finishVote();
        return;
      }
      if (control === "guess" && state.phase === "guess" && player.id === state.odd) {
        if (state.options.includes(value)) resolveGuess(value);
        return;
      }
      if (!player.host) return;
      if (control === "start" && state.phase === "lobby") beginRound();
      else if (control === "next" && state.phase === "reveal") next();
      else if (control === "restart" && state.phase === "done") restart();
    });

    // A player who leaves mid-round no longer holds the round up.
    pad.on("leave", (player) => {
      const id = player.id;
      if (!state.roster.includes(id) || !["draw", "vote", "guess"].includes(state.phase)) return;
      if (id === state.odd) return abortRound(`${nameOf(id)}, l'intruso, è uscito. Nessun punto in questo round.`);
      if (present().length < MIN_PLAYERS) return abortRound("Sono rimasti troppo pochi giocatori per finire il round.");
      if (state.phase === "draw" && id === current()) advance();
      else if (state.phase === "vote" && voteDone()) finishVote();
    });

    // ---- phones ----
    pad.layout((player) => {
      const ui = pad.ui;
      const hostButton = (id, label) => (player.host ? ui.button({ id, label, variant: "primary" }) : null);
      const mine = state.roster.includes(player.id);
      const role = () =>
        ui.column({ gap: 4 }, [
          ui.text(`Categoria: ${state.category}`, { size: "small", align: "center" }),
          player.id === state.odd
            ? ui.text("Sei l'intruso. Mimetizzati.", { size: "large", align: "center" })
            : ui.text(state.word, { size: "large", align: "center" }),
        ]);
      const scoreLine = () => ui.text(`Il tuo punteggio: ${state.scores[player.id] ?? 0}`, { size: "small", align: "center" });

      switch (state.phase) {
        case "lobby": {
          const count = pad.players.filter((p) => p.connected !== false).length;
          return ui.column({ gap: 16 }, [
            ui.text("L'Intruso", { size: "large", align: "center" }),
            ui.text(
              count < MIN_PLAYERS
                ? `In attesa di giocatori (${count} su ${MIN_PLAYERS} necessari).`
                : player.host
                  ? "Avvia quando siete tutti entrati."
                  : "Aspetta che l'host avvii la partita.",
              { align: "center" },
            ),
            count >= MIN_PLAYERS ? hostButton("start", "Inizia la partita") : null,
          ]);
        }
        case "draw": {
          if (!mine) return ui.text("C'è un round in corso. Giocherai dal prossimo.", { align: "center" });
          if (player.id === current()) {
            return ui.column({ gap: 8 }, [
              role(),
              ui.text("Tocca a te. Disegna un solo tratto.", { align: "center" }),
              ui.sketch({ id: sketchId(), colors: [state.colors[player.id]], sizes: SIZES }),
            ]);
          }
          return ui.column({ gap: 16 }, [
            role(),
            ui.text(`${nameOf(current())} sta disegnando`, { size: "large", align: "center" }),
            ui.text(`Turno ${state.step + 1} di ${total()}. Guarda lo schermo grande.`, { size: "small", align: "center" }),
          ]);
        }
        case "vote": {
          if (!mine) return ui.text("Si sta votando. Giocherai dal prossimo round.", { align: "center" });
          const mineVote = state.votes[player.id];
          if (mineVote !== undefined) {
            return ui.column({ gap: 16 }, [
              ui.text(`Hai votato ${nameOf(mineVote)}`, { size: "large", align: "center" }),
              ui.text(voted(Object.keys(state.votes).length, present().length), { align: "center" }),
            ]);
          }
          return ui.column({ gap: 12 }, [
            role(),
            ui.text("Chi è l'intruso?", { align: "center" }),
            ui.choice({
              id: "vote",
              options: present()
                .filter((id) => id !== player.id)
                .map((id) => ({ id, label: nameOf(id) })),
            }),
          ]);
        }
        case "guess": {
          if (player.id === state.odd) {
            return ui.column({ gap: 12 }, [
              ui.text(`Categoria: ${state.category}`, { size: "small", align: "center" }),
              ui.text("Ti hanno scoperto! Indovina la parola per guadagnare un punto.", { align: "center" }),
              ui.choice({ id: "guess", options: state.options.map((w) => ({ id: w, label: w })) }),
            ]);
          }
          return ui.column({ gap: 16 }, [
            ui.text(`Hanno scoperto ${nameOf(state.odd)}`, { size: "large", align: "center" }),
            ui.text("Ha un solo tentativo per indovinare la parola.", { align: "center" }),
          ]);
        }
        case "reveal": {
          const gain = state.result?.gains?.[player.id] ?? 0;
          let headline = "Round finito";
          if (state.result) {
            if (player.id === state.odd) headline = state.result.caught ? (state.result.guessRight ? "Scoperto, ma hai indovinato la parola!" : "Scoperto") : "Li hai fregati tutti!";
            else headline = state.result.caught ? (state.result.guessRight ? "Ha indovinato la parola" : "L'hai scoperto!") : `${nameOf(state.odd)} l'ha fatta franca`;
          }
          return ui.column({ gap: 16 }, [
            ui.text(headline, { size: "large", align: "center" }),
            state.aborted ? ui.text(state.aborted, { align: "center" }) : ui.text(`La parola era ${state.word}`, { align: "center" }),
            gain ? ui.text(`+${gain}`, { size: "large", align: "center" }) : null,
            scoreLine(),
            hostButton("next", isLast() ? "Classifica finale" : "Round successivo"),
          ]);
        }
        default:
          return ui.column({ gap: 16 }, [
            ui.text(`Hai totalizzato ${pts(state.scores[player.id] ?? 0)}`, { size: "large", align: "center" }),
            hostButton("restart", "Gioca ancora"),
          ]);
      }
    });

    // ---- the big screen ----
    const scoreboard = () =>
      [...pad.players]
        .sort((a, b) => (state.scores[b.id] ?? 0) - (state.scores[a.id] ?? 0))
        .map((p, i) => ctx.ui.row({ gap: 10 }, [swatch(state.colors[p.id] ?? palette[i % palette.length]), ctx.ui.text(p.name || "Giocatore"), ctx.ui.small(pts(state.scores[p.id] ?? 0))]));
    const left = () => (state.deadline ? `${Math.max(0, Math.ceil((state.deadline - state.clock) / 1000))} s` : "");
    const legend = () => ctx.ui.row({ gap: 18, wrap: true }, state.order.map((id) => ctx.ui.row({ gap: 6 }, [swatch(state.colors[id]), ctx.ui.small(nameOf(id))])));
    const votesLine = () =>
      Object.entries(state.result?.tally ?? {})
        .sort((a, b) => b[1] - a[1])
        .map(([id, n]) => ctx.ui.p(`${nameOf(id)}: ${n} ${n === 1 ? "voto" : "voti"}`));

    return () => {
      const ui = ctx.ui;
      switch (state.phase) {
        case "lobby":
          return ui.column({ gap: 16, padding: 18 }, [
            ui.h1("L'Intruso"),
            ui.p("Ognuno disegna un tratto di una parola segreta. Uno di voi non la conosce."),
            ui.p(`Entra dal telefono (da ${MIN_PLAYERS} a ${MAX_PLAYERS} giocatori). L'host avvia la partita dal suo.`),
            pad.joinCard({ size: 140 }),
            ...pad.players.map((p) => ui.p(p.name || "Giocatore")),
          ]);
        case "draw":
          return ui.column({ gap: 12, padding: 18 }, [
            ui.small(`Round ${state.round + 1} di ${ROUNDS} · Categoria: ${state.category}`),
            ui.h2(`${nameOf(current())} sta disegnando · turno ${state.step + 1} di ${total()} · ${left()}`),
            drawing(),
            legend(),
          ]);
        case "vote":
          return ui.column({ gap: 12, padding: 18 }, [
            ui.small(`Categoria: ${state.category}`),
            ui.h2(`Chi è l'intruso? ${voted(Object.keys(state.votes).length, present().length)} · ${left()}`),
            drawing(),
          ]);
        case "guess":
          return ui.column({ gap: 12, padding: 18 }, [
            ui.h2(`${nameOf(state.odd)} era l'intruso ed è stato scoperto!`),
            ui.p(`Sta provando a indovinare la parola. ${left()}`),
            ...votesLine(),
            drawing(),
          ]);
        case "reveal":
          return ui.column({ gap: 12, padding: 18 }, [
            state.aborted ? ui.h2(state.aborted) : ui.h2(`La parola era ${state.word}. L'intruso era ${nameOf(state.odd)}.`),
            state.result ? ui.p(state.result.caught ? (state.result.guessRight ? "Scoperto, ma ha indovinato la parola." : "Scoperto, e ha sbagliato la parola.") : "Non scoperto.") : null,
            ...votesLine(),
            drawing(),
            ...scoreboard(),
          ]);
        default:
          return ui.column({ gap: 10, padding: 18 }, [ui.h1("Classifica finale"), ...scoreboard()]);
      }
    };
  },
};
