export default {
  schema: "yui.simple-js.v0",
  id: "dev.genr.links",
  name: "Links",
  version: "0.1.0",
  icon: "https://img.poki-cdn.com/cdn-cgi/image/q=78,scq=50,width=80,height=44,fit=cover,f=png/c4dc286c30b8fbde45a0b5d4fe6f2146/fruit-ninja-logo.png",
  category: "Utility",
  description: "open links.genr234.com links",
  permissions: ["embed:https://links.genr234.com", "fullscreen"],

  mount(ctx) {
    const state = ctx.state({
      url: "",
      code: "",
      selected: null
    })

    function go() {
      const code = state.code.trim()
      const url = code
        ? `https://links.genr234.com/${encodeURIComponent(code)}`
        : "https://links.genr234.com/"
      state.url = url
      state.selected = true
    }

    return () =>
      ctx.ui.column({ gap: 14, padding: 18, grow: true, height: "100%" }, [
        ctx.ui.when(
          !state.selected,
          ctx.ui.row({ gap: 10, align: "center" }, [
            ctx.ui.input({
              value: state.code,
              placeholder: "insert your code here",
              onInput(value) {
                state.code = value
              },
              onKeyDown(event) {
                if (event.key === "Enter") go()
              }
            }),
            ctx.ui.button({ label: "Go!", variant: "primary", onClick: go }),
          ])
        ),

        ctx.ui.when(
          Boolean(state.selected),
          ctx.ui.embed({
            url: state.url,
            title: "Links",
            referrerPolicy: "no-referrer",
            allow: "autoplay; picture-in-picture; fullscreen",
            transport: "direct",
            width: "100%",
            height: "100%",
            minHeight: 620,
          })
        ),
      ])
  }
}
