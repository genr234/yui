export default {
  schema: "yui.simple-js.v0",
  id: "dev.genr.subway-surfers",
  name: "Subway Surfers",
  version: "0.1.0",
  icon: "https://img.poki-cdn.com/cdn-cgi/image/q=78,scq=50,width=80,height=44,fit=cover,f=png/c4dc286c30b8fbde45a0b5d4fe6f2146/subway-surfers-logo.png",
  category: "Games",
  description: "Run and jump through the subway tunnels!",
  permissions: ["embed:https://ubg77.github.io", "fullscreen"],
  
  mount(ctx) {
    return () =>
      ctx.ui.column({ gap: 12, grow: true }, [
        ctx.ui.row({ gap: 10, align: "center" }, [ctx.ui.h1("Subway Surfers")]),

        ctx.ui.embed({
          url: "https://ubg77.github.io/updatefaqs/subway-surfers-winter-holiday/",
          title: "subway surfers",
          referrerPolicy: "no-referrer",
          allow: "autoplay; picture-in-picture; fullscreen",
          blocker: true,
          credentialless: false,
          width: "100%",
          minHeight: 620,
        }),
      ]);
  }
}
