package web

import . "github.com/n0remac/GoDom/html"

func pageHead(title, extraCSS string, extraNodes ...*Node) *Node {
	nodes := []*Node{
		Meta(Charset("UTF-8")),
		Meta(Name("viewport"), Content("width=device-width, initial-scale=1.0")),
		Title(T(title)),
		Script(Src("https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4")),
		Style(T(sharedPageCSS() + extraCSS)),
	}
	nodes = append(nodes, extraNodes...)
	return Head(nodes...)
}

func uiPrimaryButtonClass(size string) string {
	return "inline-flex items-center justify-center rounded-xl border border-cyan-300/30 bg-cyan-400 px-4 font-semibold text-slate-950 shadow-sm transition duration-150 hover:-translate-y-0.5 hover:bg-cyan-300 focus:outline-none focus:ring-4 focus:ring-cyan-400/25 disabled:cursor-not-allowed disabled:opacity-60 " + uiButtonSizeClass(size)
}

func uiSecondaryButtonClass(size string) string {
	return "inline-flex items-center justify-center rounded-xl border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-4 font-medium text-[var(--app-fg)] shadow-sm transition duration-150 hover:-translate-y-0.5 hover:border-cyan-400/50 hover:bg-cyan-500/10 focus:outline-none focus:ring-4 focus:ring-cyan-400/15 disabled:cursor-not-allowed disabled:opacity-60 " + uiButtonSizeClass(size)
}

func uiDangerButtonClass(size string) string {
	return "inline-flex items-center justify-center rounded-xl border border-red-500/35 bg-red-500/10 px-4 font-medium text-red-300 shadow-sm transition duration-150 hover:-translate-y-0.5 hover:bg-red-500/16 focus:outline-none focus:ring-4 focus:ring-red-500/15 disabled:cursor-not-allowed disabled:opacity-60 " + uiButtonSizeClass(size)
}

func uiInputClass() string {
	return "w-full rounded-2xl border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-3 py-2.5 text-sm text-[var(--app-fg)] shadow-sm outline-none transition placeholder:text-[var(--app-fg-soft)] focus:border-cyan-400/70 focus:ring-4 focus:ring-cyan-400/15"
}

func uiTextareaClass() string {
	return uiInputClass() + " min-h-24 resize-y"
}

func uiSelectClass() string {
	return uiInputClass() + " pr-10"
}

func uiLabelClass() string {
	return "block text-[0.72rem] font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]"
}

func uiButtonSizeClass(size string) string {
	switch size {
	case "xs":
		return "h-8 px-3 text-[0.68rem] uppercase tracking-[0.18em]"
	case "sm":
		return "h-9 px-3.5 text-sm"
	default:
		return "h-10 px-4 text-sm"
	}
}

func sharedPageCSS() string {
	return `
:root {
  color-scheme: dark;
  --app-bg: rgb(2 6 23);
  --app-bg-depth: rgb(15 23 42);
  --app-bg-glow: rgba(34, 211, 238, 0.12);
  --app-surface: rgba(15, 23, 42, 0.74);
  --app-surface-muted: rgba(15, 23, 42, 0.9);
  --app-panel: rgb(15 23 42);
  --app-border: rgba(148, 163, 184, 0.18);
  --app-border-strong: rgba(148, 163, 184, 0.34);
  --app-fg: rgb(226 232 240);
  --app-fg-muted: rgb(203 213 225);
  --app-fg-soft: rgb(148 163 184);
}

:root[data-theme="light"] {
  color-scheme: light;
  --app-bg: rgb(241 245 249);
  --app-bg-depth: rgb(255 255 255);
  --app-bg-glow: rgba(14, 165, 233, 0.16);
  --app-surface: rgba(255, 255, 255, 0.86);
  --app-surface-muted: rgb(248 250 252);
  --app-panel: rgb(255 255 255);
  --app-border: rgba(148, 163, 184, 0.34);
  --app-border-strong: rgba(100, 116, 139, 0.46);
  --app-fg: rgb(15 23 42);
  --app-fg-muted: rgb(51 65 85);
  --app-fg-soft: rgb(100 116 139);
}

* {
  box-sizing: border-box;
}

html {
  min-height: 100%;
  background: var(--app-bg);
}

body {
  margin: 0;
  min-height: 100vh;
  background:
    radial-gradient(circle at top, var(--app-bg-glow), transparent 34%),
    linear-gradient(180deg, var(--app-bg) 0%, var(--app-bg-depth) 100%);
  color: var(--app-fg);
}
`
}
