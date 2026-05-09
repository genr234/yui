export default {
  schema: "yui.simple-js.v0",
  id: "dev.genr.fruit-ninja",
  name: "Fruit Ninja",
  version: "0.1.0",
  icon: "https://img.poki-cdn.com/cdn-cgi/image/q=78,scq=50,width=80,height=44,fit=cover,f=png/c4dc286c30b8fbde45a0b5d4fe6f2146/fruit-ninja-logo.png",
  category: "Games",
  description: "slice and dice some fruit, it's fun!",
  permissions: ["embed:https://www.coolmathgames.com", "fullscreen"],
  
  mount(ctx) {
    return () =>
      ctx.ui.column({ gap: 12, grow: true }, [
        ctx.ui.row({ gap: 10, align: "center" }, [ctx.ui.h1("Fruit Ninja")]),

        ctx.ui.embed({
          url: "https://www.coolmathgames.com/sites/default/files/public_games/50823/",
          title: "fruit ninja game",
          referrerPolicy: "no-referrer",
          allow: "autoplay; picture-in-picture; fullscreen",
          blocker: false,
          credentialless: false,
          width: "100%",
          frameHeight: "calc(100dvh - 170px)",
          minHeight: 620,
        }),
      ]);
  }
}
