const YOUTUBE_SEARCH = "https://www.youtube.com/results?hl=en&search_query="

function textFrom(value) {
  if (!value) return ""
  if (typeof value === "string") return value
  if (value.simpleText) return value.simpleText
  if (Array.isArray(value.runs)) return value.runs.map(run => run.text || "").join("")
  if (Array.isArray(value.accessibility?.accessibilityData?.label)) {
    return value.accessibility.accessibilityData.label.join("")
  }
  if (value.accessibility?.accessibilityData?.label) return value.accessibility.accessibilityData.label
  return ""
}

function bestThumbnail(thumbnail) {
  const thumbnails = thumbnail?.thumbnails || []
  return thumbnails[thumbnails.length - 1]?.url || ""
}

function compactText(value) {
  return textFrom(value).replace(/\s+/g, " ").trim()
}

function extractInitialData(html) {
  const markers = ["var ytInitialData = ", "ytInitialData = "]
  let start = -1
  let marker = ""

  for (const candidate of markers) {
    start = html.indexOf(candidate)
    if (start >= 0) {
      marker = candidate
      break
    }
  }

  if (start < 0) {
    throw new Error("could not find youtube page data")
  }

  start += marker.length
  while (/\s/.test(html[start])) start++

  let depth = 0
  let inString = false
  let escaped = false

  for (let index = start; index < html.length; index++) {
    const char = html[index]

    if (inString) {
      if (escaped) {
        escaped = false
      } else if (char === "\\") {
        escaped = true
      } else if (char === "\"") {
        inString = false
      }
      continue
    }

    if (char === "\"") {
      inString = true
    } else if (char === "{") {
      depth++
    } else if (char === "}") {
      depth--
      if (depth === 0) {
        return JSON.parse(html.slice(start, index + 1))
      }
    }
  }

  throw new Error("youtube page data was incomplete")
}

function videoFromRenderer(renderer) {
  const id = renderer.videoId
  if (!id) return null

  const title = compactText(renderer.title)
  if (!title) return null

  return {
    id,
    title,
    channel: compactText(renderer.ownerText) || compactText(renderer.shortBylineText),
    views: compactText(renderer.viewCountText),
    length: compactText(renderer.lengthText),
    published: compactText(renderer.publishedTimeText),
    thumbnail: bestThumbnail(renderer.thumbnail)
  }
}

function collectVideos(value, videos = [], seen = new Set()) {
  if (!value || typeof value !== "object") return videos

  for (const key of ["videoRenderer", "gridVideoRenderer", "compactVideoRenderer"]) {
    if (value[key]) {
      const video = videoFromRenderer(value[key])
      if (video && !seen.has(video.id)) {
        seen.add(video.id)
        videos.push(video)
      }
    }
  }

  if (Array.isArray(value)) {
    for (const item of value) collectVideos(item, videos, seen)
    return videos
  }

  for (const item of Object.values(value)) {
    collectVideos(item, videos, seen)
  }

  return videos
}

async function loadVideos(ctx, url) {
  const response = await ctx.network.fetch(url, {
    headers: {
      Accept: "text/html,application/xhtml+xml",
      "Accept-Language": "en-US,en;q=0.9"
    }
  })

  if (!response.ok) {
    throw new Error(`youtube returned ${response.status}`)
  }

  const html = await response.text()
  const data = extractInitialData(html)
  return collectVideos(data).slice(0, 24)
}

function videoIdFrom(value) {
  const text = String(value || "").trim()
  if (!text) return ""
  const watch = text.match(/[?&]v=([a-zA-Z0-9_-]{11})(?:[&#?]|$)/)
  if (watch) return watch[1]
  const short = text.match(/youtu\.be\/([a-zA-Z0-9_-]{11})(?:[/?#]|$)/)
  if (short) return short[1]
  const embed = text.match(/embed\/([a-zA-Z0-9_-]{11})(?:[/?#]|$)/)
  if (embed) return embed[1]
  const bare = text.match(/^[a-zA-Z0-9_-]{11}$/)
  return bare ? text : ""
}

export default {
  schema: "yui.simple-js.v0",
  id: "dev.genr.youtube",
  name: "youtube",
  version: "0.1.0",
  icon: "https://cdn.simpleicons.org/youtube",
  category: "Entertainment",
  description: "watch youtube from parsed home and search results",
  permissions: ["network.fetch", "embed:https://www.youtube-nocookie.com", "fullscreen"],

  mount(ctx) {
    const state = ctx.state({
      query: "",
      status: "",
      mode: "home",
      videos: [],
      selected: null,
      error: ""
    })

    async function search() {
      const query = state.query.trim()
      const id = videoIdFrom(query)

      if (id) {
        state.videos = []
        state.selected = {
          id,
          title: "youtube video",
          channel: "",
          views: "",
          length: "",
          published: "",
          thumbnail: ""
        }
        state.mode = "video"
        state.error = ""
        state.status = "ready"
        return
      }

      state.mode = "search"
      state.status = "searching"
      state.error = ""
      state.videos = []
      state.selected = null

      try {
        state.videos = await loadVideos(ctx, YOUTUBE_SEARCH + encodeURIComponent(query))
        state.selected = state.videos[0] || null
        state.status = state.videos.length ? `${state.videos.length} results` : "no results"
      } catch (error) {
        state.videos = []
        state.selected = null
        state.error = error?.message || "could not search youtube"
        state.status = "error"
      }
    }

    return () =>
      ctx.ui.column({ gap: 14, padding: 18 }, [
        ctx.ui.row({ gap: 10, align: "center" }, [
          ctx.ui.input({
            value: state.query,
            placeholder: "search youtube or paste a url",
            onInput(value) {
              state.query = value
            },
            onKeyDown(event) {
              if (event.key === "Enter") search()
            }
          }),
          ctx.ui.button({ label: "search", variant: "primary", onClick: search }),
        ]),

        ctx.ui.small(state.status),
        ctx.ui.when(Boolean(state.error), ctx.ui.small(state.error)),

        ctx.ui.when(
          Boolean(state.selected),
          ctx.ui.embed({
            url: `https://www.youtube-nocookie.com/embed/${state.selected?.id}`,
            title: state.selected?.title || "youtube video",
            referrerPolicy: "strict-origin-when-cross-origin",
            allow: "autoplay; encrypted-media; picture-in-picture",
            height: 430
          })
        ),

        ctx.ui.card({ padding: 14 }, [
          ctx.ui.h2(state.mode === "search" ? "Results" : state.mode === "video" ? "Video" : "Look something up using the search bar above"),
          ctx.ui.column(
            { gap: 10 },
            state.videos.map(video =>
              ctx.ui.row({ gap: 12, align: "center" }, [
                ctx.ui.when(
                  Boolean(video.thumbnail),
                  ctx.ui.image({
                    src: video.thumbnail,
                    alt: video.title,
                    width: 120,
                    height: 68
                  })
                ),
                ctx.ui.column({ gap: 4, grow: true }, [
                  ctx.ui.button({
                    label: video.title,
                    variant: state.selected?.id === video.id ? "primary" : "ghost",
                    onClick() {
                      state.selected = video
                    }
                  }),
                  ctx.ui.small([video.channel, video.views, video.published, video.length].filter(Boolean).join(" · "))
                ])
              ])
            )
          )
        ])
      ])
  }
}
