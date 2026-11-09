export default {
  schema: "yui.simple-js.v0",
  id: "dev.genr.party-quiz",
  name: "Party Quiz",
  version: "0.1.0",
  icon: "❓",
  category: "Games",
  description: "A quick quiz for the room. Every phone gets its own buttons, in its own order.",
  permissions: ["controllers", "library", "profiles"],

  // Declaring controllers gives this app the phones while it is open.
  controllers: {},

  mount(ctx) {
    const pad = ctx.controllers;
    const questions = [
      { q: "Which planet is closest to the Sun?", options: ["Mercury", "Venus", "Mars", "Earth"], answer: 0 },
      { q: "How many legs does a spider have?", options: ["Six", "Eight", "Ten", "Twelve"], answer: 1 },
      { q: "What is the largest ocean?", options: ["Atlantic", "Indian", "Arctic", "Pacific"], answer: 3 },
      { q: "Which gas do plants take in?", options: ["Oxygen", "Nitrogen", "Carbon dioxide", "Helium"], answer: 2 },
      { q: "How many sides does a hexagon have?", options: ["Five", "Six", "Seven", "Eight"], answer: 1 },
    ];
    const colors = ["blue", "red", "green", "yellow"];

    const state = ctx.state({
      phase: "lobby", // lobby | question | reveal | done
      round: 0,
      answers: {}, // player id → option index
      scores: {}, // player id → points
      best: {}, // player id → best score, for players who let the quiz use their Yui account
    });

    // One title in Jump back in; opening it from there resumes the round.
    const resume = ctx.launch.item?.resume;
    if (resume && typeof resume.round === "number" && resume.round < questions.length) {
      state.round = resume.round;
    }
    const publish = () =>
      ctx.library
        .publish([{ id: "classic", title: "Party Quiz", players: 8, progress: state.round / questions.length, resume: { round: state.round } }])
        .catch(() => {});
    publish();

    // Each phone sees the options in its own order (a stable shuffle per
    // player and round), so nobody can copy a neighbour's button position.
    function orderFor(playerId, round) {
      let seed = round + 1;
      for (const ch of playerId) seed = (seed * 31 + ch.charCodeAt(0)) >>> 0;
      const order = [0, 1, 2, 3];
      for (let i = order.length - 1; i > 0; i--) {
        seed = (seed * 1103515245 + 12345) >>> 0;
        const j = seed % (i + 1);
        [order[i], order[j]] = [order[j], order[i]];
      }
      return order;
    }

    const question = () => questions[state.round];
    const answeredCount = () => Object.keys(state.answers).length;

    // Haptics are a nicety (Android phones only, and the kiosk may drop them):
    // never wait on them or let them fail a round.
    const buzz = (who, name) => pad.haptic(who, name).catch(() => {});

    function startRound() {
      state.answers = {};
      state.phase = "question";
      buzz("all", "countdown"); // a new question is up
    }

    function reveal() {
      const right = question().answer;
      const scores = { ...state.scores };
      for (const [id, choice] of Object.entries(state.answers)) {
        if (choice === right) {
          scores[id] = (scores[id] ?? 0) + 1;
          buzz(id, "success");
        } else {
          buzz(id, "fail");
        }
      }
      state.scores = scores;
      state.phase = "reveal";
    }

    // Linked players keep their best score in their Yui account profile.
    // Asked once per player, on the first finish; guests and anyone who says
    // no just play on, and nothing here ever blocks the round.
    const asked = new Set();
    async function saveBest(player) {
      const profile = ctx.profiles?.for(player.id);
      if (!profile) return;
      const score = state.scores[player.id] ?? 0;
      if (!asked.has(player.id)) {
        asked.add(player.id);
        const { granted } = await profile.request();
        if (!granted) return;
      }
      for (let attempt = 0; attempt < 2; attempt++) {
        const { data, version } = await profile.get();
        const best = Math.max(score, Number(data?.best) || 0);
        try {
          await profile.set({ ...(data ?? {}), best }, version);
        } catch (error) {
          if (error?.code === "version_conflict") continue; // another kiosk saved: re-read and merge
          throw error;
        }
        state.best = { ...state.best, [player.id]: best };
        return;
      }
    }
    function saveBests() {
      for (const player of pad.players) {
        saveBest(player).catch((error) => {
          // not_granted / not_linked: the player chose not to, or left the account.
          if (error?.code !== "not_granted" && error?.code !== "not_linked") ctx.log.warn("profile:", error);
        });
      }
    }

    function next() {
      if (state.round + 1 >= questions.length) {
        state.phase = "done";
        const top = Math.max(0, ...pad.players.map((p) => state.scores[p.id] ?? 0));
        for (const p of pad.players) buzz(p.id, top > 0 && (state.scores[p.id] ?? 0) === top ? "win" : "tap");
        saveBests();
        ctx.library.played("classic", { progress: 1, resume: { round: 0 } }).catch(() => {});
        return;
      }
      state.round += 1;
      ctx.library.played("classic", { progress: state.round / questions.length, resume: { round: state.round } }).catch(() => {});
      startRound();
    }

    function restart() {
      state.round = 0;
      state.scores = {};
      state.best = {};
      publish();
      startRound();
    }

    // Per-player layouts: re-run whenever ctx.state or the players change.
    pad.layout((player) => {
      const ui = pad.ui;
      const hostButton = (id, label) => (player.host ? ui.button({ id, label, variant: "primary" }) : null);
      switch (state.phase) {
        case "lobby":
          return ui.column({ gap: 16 }, [
            ui.text("Party Quiz", { size: "large", align: "center" }),
            ui.text(player.host ? "Start when everyone has joined." : "Waiting for the host to start.", { align: "center" }),
            hostButton("start", "Start quiz"),
          ]);
        case "question": {
          const mine = state.answers[player.id];
          if (mine !== undefined) {
            return ui.column({ gap: 16 }, [
              ui.text(`Locked in: ${question().options[mine]}`, { size: "large", align: "center" }),
              ui.text(`${answeredCount()} of ${pad.players.length} answered`, { align: "center" }),
              hostButton("reveal", "Reveal answer"),
            ]);
          }
          return ui.column({ gap: 12 }, [
            ui.text(`Question ${state.round + 1}`, { size: "small", align: "center" }),
            ui.text(question().q, { align: "center" }),
            ui.choice({
              id: "answer",
              options: orderFor(player.id, state.round).map((index, slot) => ({
                id: String(index),
                label: question().options[index],
                color: colors[slot],
              })),
            }),
            hostButton("reveal", "Reveal answer"),
          ]);
        }
        case "reveal": {
          const right = state.answers[player.id] === question().answer;
          return ui.column({ gap: 16 }, [
            ui.text(right ? "Correct!" : "Not quite", { size: "large", align: "center" }),
            ui.text(`Answer: ${question().options[question().answer]}`, { align: "center" }),
            ui.text(`Your score: ${state.scores[player.id] ?? 0}`, { size: "small", align: "center" }),
            hostButton("next", state.round + 1 >= questions.length ? "Final scores" : "Next question"),
          ]);
        }
        default:
          return ui.column({ gap: 16 }, [
            ui.text(`You scored ${state.scores[player.id] ?? 0}`, { size: "large", align: "center" }),
            state.best[player.id] !== undefined ? ui.text(`Your best: ${state.best[player.id]}`, { align: "center" }) : null,
            hostButton("restart", "Play again"),
          ]);
      }
    });

    pad.on("event", ({ player, control, kind, value }) => {
      if (kind !== "tap") return;
      if (control === "answer" && state.phase === "question" && state.answers[player.id] === undefined) {
        state.answers = { ...state.answers, [player.id]: Number(value) };
        return;
      }
      if (!player.host) return;
      if (control === "start" && state.phase === "lobby") startRound();
      else if (control === "reveal" && state.phase === "question") reveal();
      else if (control === "next" && state.phase === "reveal") next();
      else if (control === "restart" && state.phase === "done") restart();
    });

    // A player who leaves mid-round no longer holds the round up.
    pad.on("leave", (player) => {
      if (state.answers[player.id] === undefined) return;
      const answers = { ...state.answers };
      delete answers[player.id];
      state.answers = answers;
    });

    const scoreboard = () =>
      [...pad.players]
        .sort((a, b) => (state.scores[b.id] ?? 0) - (state.scores[a.id] ?? 0))
        .map((p) => ctx.ui.row({ gap: 10 }, [ctx.ui.text(p.name || "Player"), ctx.ui.small(`${state.scores[p.id] ?? 0} pts`)]));

    return () =>
      ctx.ui.column({ gap: 16, padding: 18 }, [
        ctx.ui.h1("Party Quiz"),
        ctx.ui.when(state.phase === "lobby", ctx.ui.column({ gap: 12 }, [
          ctx.ui.p("Join from your phone. The host starts the quiz from theirs."),
          pad.joinCard({ size: 140 }),
        ])),
        ctx.ui.when(state.phase === "question", ctx.ui.column({ gap: 10 }, [
          ctx.ui.small(`Question ${state.round + 1} of ${questions.length}`),
          ctx.ui.h2(question().q),
          ctx.ui.p(`${answeredCount()} of ${pad.players.length} players answered`),
        ])),
        ctx.ui.when(state.phase === "reveal", ctx.ui.column({ gap: 10 }, [
          ctx.ui.h2(`Answer: ${question().options[question().answer]}`),
          ...scoreboard(),
        ])),
        ctx.ui.when(state.phase === "done", ctx.ui.column({ gap: 10 }, [ctx.ui.h2("Final scores"), ...scoreboard()])),
      ]);
  },
};
