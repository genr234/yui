// Dev check for the letterbox bars: a 16:9 page in a 9:16 kiosk, fullscreen.
export default {
  schema: "yui.simple-js.v0",
  id: "dev.genr.bars-demo",
  name: "Bars Demo",
  version: "0.1.0",
  icon: "📺",
  category: "Games",
  description: "A 16:9 test card that shows the letterbox bars. Enter fullscreen from the app bar.",
  permissions: ["fullscreen"],
  web: {},

  mount(ctx) {
    return () =>
      ctx.ui.column({ gap: 0, grow: true }, [
        ctx.ui.embed({ web: "index.html", fill: true, aspect: "16:9", title: "Test card" }),
      ]);
  },
};
