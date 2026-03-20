package web

import (
	"context"
	"fmt"
	stdhtml "html"
	"net/http"
	"strings"
	"sync"

	. "github.com/n0remac/GoDom/html"
	godomws "github.com/n0remac/GoDom/websocket"

	"github.com/n0remac/Knowledge-Graph/internal/adminstream"
	"github.com/n0remac/Knowledge-Graph/internal/conversation"
	"github.com/n0remac/Knowledge-Graph/internal/embeddingtest"
	"github.com/n0remac/Knowledge-Graph/internal/memory"
)

const (
	adminRoomID                = "admin-dashboard"
	adminConversationSectionID = "admin-vertical-conversation"
	adminMemorySectionID       = "admin-vertical-memory"
	adminEmbeddingsSectionID   = "admin-vertical-embeddings"
)

type AdminDependencies struct {
	ConversationStore *conversation.Store
	MemoryStore       *memory.Store
	EmbeddingService  *embeddingtest.Service
	EmbeddingInitErr  error
	Notifier          *adminstream.Notifier
}

var (
	adminStreamMu      sync.Mutex
	adminStreamStarted = make(map[*adminstream.Notifier]bool)
)

func Admin(mux *http.ServeMux, deps AdminDependencies) {
	startAdminStream(deps)
	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		ServeNode(AdminPage(deps))(w, r)
	})
}

func AdminPage(deps AdminDependencies) *Node {
	return Html(
		Head(
			Meta(Charset("UTF-8")),
			Meta(Name("viewport"), Content("width=device-width, initial-scale=1.0")),
			Title(T("Admin Dashboard")),
			DaisyUI,
			Script(Src("https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4")),
			Script(Src("https://unpkg.com/htmx.org@2.0.4")),
			Script(Src("https://unpkg.com/htmx-ext-ws@2.0.2/ws.js")),
			Style(T(adminPageCSS())),
		),
		Body(
			Attr("data-theme", "corporate"),
			Attr("hx-ext", "ws"),
			Attr("ws-connect", "/ws/hub?room="+adminRoomID),
			Class("min-h-screen bg-slate-100 text-slate-900"),
			Div(
				Class("mx-auto flex min-h-screen w-full max-w-[1800px] flex-col px-4 py-5 md:px-6"),
				Header(
					Class("rounded-[2rem] border border-slate-300/80 bg-white/90 px-6 py-5 shadow-sm"),
					Div(
						Class("flex flex-wrap items-center gap-3"),
						Div(
							Class("mr-auto"),
							H1(Class("text-3xl font-semibold tracking-tight text-slate-900"), T("Storage Admin Dashboard")),
							P(Class("mt-2 text-sm text-slate-600"), T("Live view of conversation, memory, and embedding storage verticals.")),
						),
						Span(Class("rounded-full bg-emerald-100 px-3 py-1 text-xs font-semibold uppercase tracking-[0.2em] text-emerald-700"), T("Live via HTMX WebSocket")),
					),
				),
				Main(
					Class("mt-5 grid flex-1 gap-4 xl:grid-cols-3"),
					renderMemorySection(deps.MemoryStore),
					renderEmbeddingsSection(deps.EmbeddingService, deps.EmbeddingInitErr),
				),
			),
		),
	)
}

func adminPageCSS() string {
	return `
body { margin: 0; }
.admin-vertical { min-height: 22rem; }
.admin-scroll { max-height: 24rem; overflow-y: auto; }
.admin-code { white-space: pre-wrap; word-break: break-word; }
`
}

func renderMemorySection(store *memory.Store) *Node {
	title := "Memory Store"
	description := "SQLite passive ingestion pipeline"
	if store == nil {
		return adminSection(adminMemorySectionID, title, description, `<p class="text-sm text-slate-500">Memory collector is disabled.</p>`)
	}

	snapshot, err := store.Snapshot(context.Background(), memory.SnapshotOptions{
		RecentMessages:        5,
		RecentClaims:          5,
		RecentVectorDocuments: 5,
	})
	if err != nil {
		return adminSection(adminMemorySectionID, title, description, `<p class="text-sm text-red-600">`+escapeHTML(err.Error())+`</p>`)
	}

	var builder strings.Builder
	builder.WriteString(adminStatsRow([]adminStat{
		{Label: "Messages", Value: fmt.Sprintf("%d", snapshot.MessageCount)},
		{Label: "Claims", Value: fmt.Sprintf("%d", snapshot.ClaimCount)},
		{Label: "Vectors", Value: fmt.Sprintf("%d", snapshot.VectorDocumentCount)},
	}))
	builder.WriteString(`<div class="mt-4 grid gap-4 lg:grid-cols-3">`)
	builder.WriteString(adminSubsection("Recent Messages", renderMemoryMessages(snapshot.RecentMessages)))
	builder.WriteString(adminSubsection("Recent Claims", renderMemoryClaims(snapshot.RecentClaims)))
	builder.WriteString(adminSubsection("Recent Vector Docs", renderMemoryVectorDocuments(snapshot.RecentVectorDocs)))
	builder.WriteString(`</div>`)
	return adminSection(adminMemorySectionID, title, description, builder.String())
}

func renderEmbeddingsSection(service *embeddingtest.Service, initErr error) *Node {
	title := "Embeddings Store"
	description := "Filesystem + Qdrant embedding test slice"
	switch {
	case initErr != nil:
		return adminSection(adminEmbeddingsSectionID, title, description, `<p class="text-sm text-red-600">`+escapeHTML(initErr.Error())+`</p>`)
	case service == nil:
		return adminSection(adminEmbeddingsSectionID, title, description, `<p class="text-sm text-slate-500">Embeddings service is not available.</p>`)
	}

	messageSets, err := service.ListMessageSets()
	if err != nil {
		return adminSection(adminEmbeddingsSectionID, title, description, `<p class="text-sm text-red-600">`+escapeHTML(err.Error())+`</p>`)
	}
	runs, err := service.ListRuns(5)
	if err != nil {
		return adminSection(adminEmbeddingsSectionID, title, description, `<p class="text-sm text-red-600">`+escapeHTML(err.Error())+`</p>`)
	}

	var builder strings.Builder
	builder.WriteString(adminStatsRow([]adminStat{
		{Label: "Default Model", Value: service.Defaults().EmbeddingModel},
		{Label: "Message Sets", Value: fmt.Sprintf("%d", len(messageSets))},
		{Label: "Runs", Value: fmt.Sprintf("%d", len(runs))},
	}))
	builder.WriteString(`<div class="mt-4 grid gap-4 lg:grid-cols-2">`)
	builder.WriteString(adminSubsection("Recent Message Sets", renderEmbeddingSets(messageSets)))
	builder.WriteString(adminSubsection("Recent Runs", renderEmbeddingRuns(runs)))
	builder.WriteString(`</div>`)

	return adminSection(adminEmbeddingsSectionID, title, description, builder.String())
}

func adminSection(id, title, description, bodyHTML string) *Node {
	return Section(
		Id(id),
		Class("admin-vertical rounded-[2rem] border border-slate-300/80 bg-white p-5 shadow-sm"),
		Div(
			Class("flex items-start justify-between gap-3"),
			Div(
				H2(Class("text-xl font-semibold text-slate-900"), T(title)),
				P(Class("mt-1 text-sm text-slate-500"), T(description)),
			),
		),
		Div(Class("mt-4"), Raw(bodyHTML)),
	)
}

type adminStat struct {
	Label string
	Value string
}

func adminStatsRow(stats []adminStat) string {
	var builder strings.Builder
	builder.WriteString(`<div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">`)
	for _, stat := range stats {
		builder.WriteString(`<div class="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3">`)
		builder.WriteString(`<p class="text-[0.7rem] font-semibold uppercase tracking-[0.2em] text-slate-500">` + escapeHTML(stat.Label) + `</p>`)
		builder.WriteString(`<p class="mt-2 text-lg font-semibold text-slate-900">` + escapeHTML(stat.Value) + `</p>`)
		builder.WriteString(`</div>`)
	}
	builder.WriteString(`</div>`)
	return builder.String()
}

func adminSubsection(title, body string) string {
	return `<section class="rounded-2xl border border-slate-200 bg-slate-50 p-4">` +
		`<h3 class="text-sm font-semibold uppercase tracking-[0.18em] text-slate-500">` + escapeHTML(title) + `</h3>` +
		`<div class="admin-scroll mt-3 space-y-3">` + body + `</div>` +
		`</section>`
}

func renderMemoryMessages(items []memory.MessageSummary) string {
	if len(items) == 0 {
		return `<p class="text-sm text-slate-500">No stored messages.</p>`
	}
	var builder strings.Builder
	for _, item := range items {
		builder.WriteString(`<article class="rounded-2xl border border-slate-200 bg-white px-4 py-3">`)
		builder.WriteString(`<p class="text-xs uppercase tracking-[0.18em] text-slate-500">` + escapeHTML(item.ConversationID) + ` • ` + escapeHTML(item.AuthorRole) + `</p>`)
		builder.WriteString(`<p class="mt-2 text-sm text-slate-800">` + escapeHTML(item.Content) + `</p>`)
		builder.WriteString(`</article>`)
	}
	return builder.String()
}

func renderMemoryClaims(items []memory.ClaimRecord) string {
	if len(items) == 0 {
		return `<p class="text-sm text-slate-500">No stored claims.</p>`
	}
	var builder strings.Builder
	for _, item := range items {
		builder.WriteString(`<article class="rounded-2xl border border-slate-200 bg-white px-4 py-3">`)
		builder.WriteString(`<p class="text-xs uppercase tracking-[0.18em] text-slate-500">` + escapeHTML(item.ConversationID) + `</p>`)
		builder.WriteString(`<p class="mt-2 text-sm font-medium text-slate-900">` + escapeHTML(item.Subject+" | "+item.Predicate+" | "+item.Object) + `</p>`)
		builder.WriteString(`</article>`)
	}
	return builder.String()
}

func renderMemoryVectorDocuments(items []memory.VectorDocumentRecord) string {
	if len(items) == 0 {
		return `<p class="text-sm text-slate-500">No vector documents stored.</p>`
	}
	var builder strings.Builder
	for _, item := range items {
		builder.WriteString(`<article class="rounded-2xl border border-slate-200 bg-white px-4 py-3">`)
		builder.WriteString(`<div class="flex items-center justify-between gap-3">`)
		builder.WriteString(`<p class="text-xs uppercase tracking-[0.18em] text-slate-500">` + escapeHTML(item.Kind) + `</p>`)
		builder.WriteString(`<span class="rounded-full bg-slate-900 px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-white">` + escapeHTML(item.IndexStatus) + `</span>`)
		builder.WriteString(`</div>`)
		builder.WriteString(`<p class="mt-2 text-sm text-slate-800">` + escapeHTML(item.Content) + `</p>`)
		builder.WriteString(`</article>`)
	}
	return builder.String()
}

func renderEmbeddingSets(items []embeddingtest.MessageSet) string {
	if len(items) == 0 {
		return `<p class="text-sm text-slate-500">No message sets stored.</p>`
	}
	var builder strings.Builder
	for _, item := range items {
		builder.WriteString(`<article class="rounded-2xl border border-slate-200 bg-white px-4 py-3">`)
		builder.WriteString(`<h4 class="font-semibold text-slate-900">` + escapeHTML(item.Name) + `</h4>`)
		builder.WriteString(`<p class="mt-2 text-sm text-slate-700">` + fmt.Sprintf("%d messages", len(item.Messages)) + `</p>`)
		builder.WriteString(`</article>`)
	}
	return builder.String()
}

func renderEmbeddingRuns(items []embeddingtest.RunRecord) string {
	if len(items) == 0 {
		return `<p class="text-sm text-slate-500">No embedding runs recorded.</p>`
	}
	var builder strings.Builder
	for _, item := range items {
		builder.WriteString(`<article class="rounded-2xl border border-slate-200 bg-white px-4 py-3">`)
		builder.WriteString(`<div class="flex items-center justify-between gap-3">`)
		builder.WriteString(`<h4 class="font-semibold text-slate-900">` + escapeHTML(item.MessageSetName) + `</h4>`)
		builder.WriteString(`<span class="rounded-full bg-slate-900 px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-white">` + escapeHTML(item.Status) + `</span>`)
		builder.WriteString(`</div>`)
		builder.WriteString(`<p class="mt-2 text-sm text-slate-700">` + escapeHTML(item.Query) + `</p>`)
		builder.WriteString(`</article>`)
	}
	return builder.String()
}

func startAdminStream(deps AdminDependencies) {
	if deps.Notifier == nil {
		return
	}
	adminStreamMu.Lock()
	if adminStreamStarted[deps.Notifier] {
		adminStreamMu.Unlock()
		return
	}
	adminStreamStarted[deps.Notifier] = true
	adminStreamMu.Unlock()

	events := deps.Notifier.Subscribe(8)
	go func() {
		for event := range events {
			broadcastAdminVertical(event.Vertical, deps)
		}
	}()
}

func broadcastAdminVertical(vertical string, deps AdminDependencies) {
	var node *Node
	switch vertical {
	case adminstream.VerticalMemory:
		node = renderMemorySection(deps.MemoryStore)
	case adminstream.VerticalEmbeddings:
		node = renderEmbeddingsSection(deps.EmbeddingService, deps.EmbeddingInitErr)
	default:
		return
	}
	godomws.WsHub.Broadcast <- godomws.WebsocketMessage{
		Room:    adminRoomID,
		Content: []byte(node.Render()),
	}
}

func escapeHTML(value string) string {
	return stdhtml.EscapeString(strings.TrimSpace(value))
}
