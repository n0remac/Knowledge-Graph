package web

import . "github.com/n0remac/GoDom/html"

func GraphPage() *Node {
	return Html(
		Head(
			Meta(Charset("UTF-8")),
			Meta(
				Name("viewport"),
				Content("width=device-width, initial-scale=1.0"),
			),
			Title(T("Knowledge Graph")),
			DaisyUI,
			Script(Src("https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4")),
			Style(T(graphPageCSS())),
		),
		Body(
			Attr("data-theme", "corporate"),
			Class("min-h-screen bg-slate-100 text-slate-900"),
			Div(
				Class("flex min-h-screen flex-col"),
				GraphToolbar(),
				Main(
					Class("flex-1 px-4 pb-4 pt-3 md:px-6"),
					Div(
						Id("graph-canvas"),
						Class("h-full w-full overflow-hidden rounded-3xl border border-slate-300/80 bg-white shadow-sm"),
						Attr("style", "min-height: calc(100vh - 116px);"),
					),
				),
			),
			GraphScripts(),
		),
	)
}

func GraphToolbar() *Node {
	return Header(
		Class("border-b border-slate-300/80 bg-white/90 backdrop-blur"),
		Div(
			Class("mx-auto flex w-full max-w-none flex-wrap items-center gap-3 px-4 py-4 md:px-6"),
			Div(
				Class("mr-auto flex min-w-[14rem] flex-col"),
				H1(
					Class("text-2xl font-semibold tracking-tight text-slate-900"),
					T("Knowledge Graph"),
				),
				P(
					Class("text-sm text-slate-600"),
					T("Complete graph viewer with Cytoscape.js"),
				),
			),
			Button(
				Id("graph-reload-btn"),
				Type("button"),
				Class("btn btn-sm border-slate-300 bg-white text-slate-900 hover:border-cyan-500 hover:bg-cyan-50"),
				T("Reload"),
			),
			Label(
				Class("form-control flex items-center gap-2"),
				Span(
					Class("text-sm font-medium text-slate-700"),
					T("Layout"),
				),
				Select(
					Id("graph-layout-select"),
					Class("select select-sm select-bordered min-w-40 border-slate-300 bg-white"),
					Option(Value("breadthfirst"), T("breadthfirst")),
					Option(Value("cose"), T("cose")),
					Option(Value("concentric"), T("concentric")),
					Option(Value("circle"), T("circle")),
					Option(Value("grid"), T("grid")),
				),
			),
			Form(
				Id("graph-search-form"),
				Class("flex flex-1 min-w-[18rem] items-center gap-2"),
				Input(
					Id("graph-search-input"),
					Type("search"),
					Class("input input-sm input-bordered w-full border-slate-300 bg-white"),
					Placeholder("Search nodes, facts, topics, messages"),
					Attr("autocomplete", "off"),
				),
				Button(
					Type("submit"),
					Class("btn btn-sm bg-slate-900 text-white hover:bg-cyan-700"),
					T("Search"),
				),
			),
			Label(
				Class("label flex cursor-pointer items-center gap-2 rounded-full border border-slate-300 bg-white px-3 py-2"),
				Input(
					Id("graph-hide-messages-toggle"),
					Type("checkbox"),
					Class("checkbox checkbox-sm border-slate-400"),
				),
				Span(
					Class("label-text text-sm text-slate-700"),
					T("Hide messages"),
				),
			),
			Div(
				Id("graph-status"),
				Class("text-sm text-slate-500"),
				Attr("role", "status"),
				T("Loading graph..."),
			),
		),
	)
}

func graphPageCSS() string {
	return `
:root {
  color-scheme: light;
}

body {
  margin: 0;
}

#graph-canvas {
  background:
    radial-gradient(circle at top left, rgba(34, 211, 238, 0.14), transparent 28rem),
    radial-gradient(circle at bottom right, rgba(245, 158, 11, 0.14), transparent 24rem),
    linear-gradient(180deg, rgba(248, 250, 252, 1) 0%, rgba(241, 245, 249, 1) 100%);
}
`
}
