package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"

	. "github.com/n0remac/GoDom/html"
	godomws "github.com/n0remac/GoDom/websocket"

	"github.com/n0remac/Knowledge-Graph/internal/adminstream"
	"github.com/n0remac/Knowledge-Graph/internal/embeddingtest"
	"github.com/n0remac/Knowledge-Graph/internal/memory"
)

const (
	adminRoomID              = "admin-dashboard"
	adminMemorySectionID     = "admin-vertical-memory"
	adminEmbeddingsSectionID = "admin-vertical-embeddings"
)

type AdminDependencies struct {
	MemoryStore      *memory.Store
	EmbeddingService *embeddingtest.Service
	EmbeddingInitErr error
	Notifier         *adminstream.Notifier
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
							P(Class("mt-2 text-sm text-slate-600"), T("Live view of memory and embedding storage verticals.")),
						),
						Span(Class("rounded-full bg-emerald-100 px-3 py-1 text-xs font-semibold uppercase tracking-[0.2em] text-emerald-700"), T("Live via HTMX WebSocket")),
					),
				),
				Main(
					Class("mt-5 grid flex-1 gap-4 xl:grid-cols-2"),
					renderMemorySection(deps.MemoryStore),
					renderEmbeddingsSection(deps.MemoryStore, deps.EmbeddingService, deps.EmbeddingInitErr),
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
		return adminSection(
			adminMemorySectionID,
			title,
			description,
			P(Class("text-sm text-slate-500"), T("Memory collector is disabled.")),
		)
	}

	snapshot, err := store.Snapshot(context.Background(), memory.SnapshotOptions{
		RecentMessages:        5,
		RecentClaims:          5,
		RecentVectorDocuments: 5,
	})
	if err != nil {
		return adminSection(
			adminMemorySectionID,
			title,
			description,
			P(Class("text-sm text-red-600"), T(err.Error())),
		)
	}

	return adminSection(
		adminMemorySectionID,
		title,
		description,
		adminStatsRow([]adminStat{
			{Label: "Messages", Value: strconv.FormatInt(snapshot.MessageCount, 10)},
			{Label: "Claims", Value: strconv.FormatInt(snapshot.ClaimCount, 10)},
			{Label: "Vectors", Value: strconv.FormatInt(snapshot.VectorDocumentCount, 10)},
		}),
		Div(
			Class("mt-4 grid gap-4 lg:grid-cols-3"),
			adminSubsection("Recent Messages", renderMemoryMessages(snapshot.RecentMessages)),
			adminSubsection("Recent Claims", renderMemoryClaims(snapshot.RecentClaims)),
			adminSubsection("Recent Vector Docs", renderMemoryVectorDocuments(snapshot.RecentVectorDocs)),
		),
	)
}

func renderEmbeddingsSection(store *memory.Store, service *embeddingtest.Service, initErr error) *Node {
	title := "Embeddings Store"
	description := "Live Qdrant-backed vector index plus embedding test slice"

	var (
		liveSnapshot memory.Snapshot
		liveErr      error
		messageSets  []embeddingtest.MessageSet
		runs         []embeddingtest.RunRecord
	)

	if store != nil {
		liveSnapshot, liveErr = store.Snapshot(context.Background(), memory.SnapshotOptions{
			RecentVectorDocuments: 5,
		})
	}
	if service != nil {
		messageSets, liveErr = service.ListMessageSets()
		if liveErr == nil {
			runs, liveErr = service.ListRuns(5)
		}
	}

	if liveErr != nil {
		return adminSection(
			adminEmbeddingsSectionID,
			title,
			description,
			P(Class("text-sm text-red-600"), T(liveErr.Error())),
		)
	}
	if initErr != nil && store == nil && service == nil {
		return adminSection(
			adminEmbeddingsSectionID,
			title,
			description,
			P(Class("text-sm text-red-600"), T(initErr.Error())),
		)
	}

	stats := []adminStat{{Label: "Live Vectors", Value: strconv.FormatInt(liveSnapshot.VectorDocumentCount, 10)}}
	if model := embeddingDefaultsLabel(store, service, liveSnapshot); model != "" {
		stats = append(stats, adminStat{Label: "Default Model", Value: model})
	}
	if collection := recentCollectionLabel(liveSnapshot.RecentVectorDocs); collection != "" {
		stats = append(stats, adminStat{Label: "Collection", Value: collection})
	} else {
		stats = append(stats, adminStat{Label: "Message Sets", Value: strconv.Itoa(len(messageSets))})
	}

	return adminSection(
		adminEmbeddingsSectionID,
		title,
		description,
		adminStatsRow(stats),
		Div(
			Class("mt-4 grid gap-4 lg:grid-cols-2"),
			adminSubsection("Live Vector Docs", renderEmbeddingVectorDocuments(liveSnapshot.RecentVectorDocs, initErr)),
			adminSubsection("Embedding Test Slice", renderEmbeddingTestOverview(messageSets, runs, initErr)),
		),
	)
}

func adminSection(id, title, description string, body ...*Node) *Node {
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
		Div(Class("mt-4"), Ch(body)),
	)
}

type adminStat struct {
	Label string
	Value string
}

func adminStatsRow(stats []adminStat) *Node {
	cards := make([]*Node, 0, len(stats))
	for _, stat := range stats {
		cards = append(cards, Div(
			Class("rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3"),
			P(Class("text-[0.7rem] font-semibold uppercase tracking-[0.2em] text-slate-500"), T(stat.Label)),
			P(Class("mt-2 text-lg font-semibold text-slate-900"), T(stat.Value)),
		))
	}
	return Div(Class("grid gap-3 sm:grid-cols-2 xl:grid-cols-3"), Ch(cards))
}

func adminSubsection(title string, body []*Node) *Node {
	return Section(
		Class("rounded-2xl border border-slate-200 bg-slate-50 p-4"),
		H3(Class("text-sm font-semibold uppercase tracking-[0.18em] text-slate-500"), T(title)),
		Div(Class("admin-scroll mt-3 space-y-3"), Ch(body)),
	)
}

func renderMemoryMessages(items []memory.MessageSummary) []*Node {
	if len(items) == 0 {
		return []*Node{P(Class("text-sm text-slate-500"), T("No stored messages."))}
	}
	nodes := make([]*Node, 0, len(items))
	for _, item := range items {
		nodes = append(nodes, Article(
			Class("rounded-2xl border border-slate-200 bg-white px-4 py-3"),
			P(Class("text-xs uppercase tracking-[0.18em] text-slate-500"), T(item.ConversationID+" • "+item.AuthorRole)),
			P(Class("mt-2 text-sm text-slate-800"), T(item.Content)),
		))
	}
	return nodes
}

func renderMemoryClaims(items []memory.ClaimRecord) []*Node {
	if len(items) == 0 {
		return []*Node{P(Class("text-sm text-slate-500"), T("No stored claims."))}
	}
	nodes := make([]*Node, 0, len(items))
	for _, item := range items {
		nodes = append(nodes, Article(
			Class("rounded-2xl border border-slate-200 bg-white px-4 py-3"),
			P(Class("text-xs uppercase tracking-[0.18em] text-slate-500"), T(item.ConversationID)),
			P(Class("mt-2 text-sm font-medium text-slate-900"), T(item.Subject+" | "+item.Predicate+" | "+item.Object)),
		))
	}
	return nodes
}

func renderMemoryVectorDocuments(items []memory.VectorDocumentRecord) []*Node {
	if len(items) == 0 {
		return []*Node{P(Class("text-sm text-slate-500"), T("No vector documents stored."))}
	}
	nodes := make([]*Node, 0, len(items))
	for _, item := range items {
		nodes = append(nodes, Article(
			Class("rounded-2xl border border-slate-200 bg-white px-4 py-3"),
			Div(
				Class("flex items-center justify-between gap-3"),
				P(Class("text-xs uppercase tracking-[0.18em] text-slate-500"), T(item.Kind)),
				Span(Class("rounded-full bg-slate-900 px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-white"), T(item.IndexStatus)),
			),
			P(Class("mt-2 text-sm text-slate-800"), T(item.Content)),
		))
	}
	return nodes
}

func renderEmbeddingSets(items []embeddingtest.MessageSet) []*Node {
	if len(items) == 0 {
		return []*Node{P(Class("text-sm text-slate-500"), T("No message sets stored."))}
	}
	nodes := make([]*Node, 0, len(items))
	for _, item := range items {
		nodes = append(nodes, Article(
			Class("rounded-2xl border border-slate-200 bg-white px-4 py-3"),
			H4(Class("font-semibold text-slate-900"), T(item.Name)),
			P(Class("mt-2 text-sm text-slate-700"), T(strconv.Itoa(len(item.Messages))+" messages")),
		))
	}
	return nodes
}

func renderEmbeddingRuns(items []embeddingtest.RunRecord) []*Node {
	if len(items) == 0 {
		return []*Node{P(Class("text-sm text-slate-500"), T("No embedding runs recorded."))}
	}
	nodes := make([]*Node, 0, len(items))
	for _, item := range items {
		nodes = append(nodes, Article(
			Class("rounded-2xl border border-slate-200 bg-white px-4 py-3"),
			Div(
				Class("flex items-center justify-between gap-3"),
				H4(Class("font-semibold text-slate-900"), T(item.MessageSetName)),
				Span(Class("rounded-full bg-slate-900 px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-white"), T(item.Status)),
			),
			P(Class("mt-2 text-sm text-slate-700"), T(item.Query)),
		))
	}
	return nodes
}

func renderEmbeddingVectorDocuments(items []memory.VectorDocumentRecord, initErr error) []*Node {
	if len(items) == 0 {
		if initErr != nil {
			return []*Node{P(Class("text-sm text-red-600"), T(initErr.Error()))}
		}
		return []*Node{P(Class("text-sm text-slate-500"), T("No live vector documents indexed yet."))}
	}

	nodes := make([]*Node, 0, len(items))
	for _, item := range items {
		metadata := strings.TrimSpace(item.EmbeddingModel)
		if collection := strings.TrimSpace(item.CollectionName); collection != "" {
			if metadata != "" {
				metadata += " • "
			}
			metadata += collection
		}
		nodes = append(nodes, Article(
			Class("rounded-2xl border border-slate-200 bg-white px-4 py-3"),
			Div(
				Class("flex items-center justify-between gap-3"),
				P(Class("text-xs uppercase tracking-[0.18em] text-slate-500"), T(item.Kind)),
				Span(Class("rounded-full bg-slate-900 px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-white"), T(item.IndexStatus)),
			),
			P(Class("mt-2 text-sm text-slate-800"), T(item.Content)),
			P(Class("mt-2 text-xs text-slate-500"), T(metadata)),
		))
	}
	return nodes
}

func renderEmbeddingTestOverview(messageSets []embeddingtest.MessageSet, runs []embeddingtest.RunRecord, initErr error) []*Node {
	if initErr != nil {
		return []*Node{P(Class("text-sm text-red-600"), T(initErr.Error()))}
	}
	if len(messageSets) == 0 && len(runs) == 0 {
		return []*Node{P(Class("text-sm text-slate-500"), T("No embedding test message sets or runs yet."))}
	}

	nodes := make([]*Node, 0, len(messageSets)+len(runs))
	for _, item := range messageSets {
		nodes = append(nodes, Article(
			Class("rounded-2xl border border-slate-200 bg-white px-4 py-3"),
			H4(Class("font-semibold text-slate-900"), T(item.Name)),
			P(Class("mt-2 text-sm text-slate-700"), T(strconv.Itoa(len(item.Messages))+" messages")),
		))
	}
	for _, item := range runs {
		nodes = append(nodes, Article(
			Class("rounded-2xl border border-slate-200 bg-white px-4 py-3"),
			Div(
				Class("flex items-center justify-between gap-3"),
				H4(Class("font-semibold text-slate-900"), T(item.MessageSetName)),
				Span(Class("rounded-full bg-slate-900 px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-white"), T(item.Status)),
			),
			P(Class("mt-2 text-sm text-slate-700"), T(item.Query)),
		))
	}
	return nodes
}

func embeddingDefaultsLabel(store *memory.Store, service *embeddingtest.Service, snapshot memory.Snapshot) string {
	if service != nil {
		if model := strings.TrimSpace(service.Defaults().EmbeddingModel); model != "" {
			return model
		}
	}
	for _, item := range snapshot.RecentVectorDocs {
		if model := strings.TrimSpace(item.EmbeddingModel); model != "" {
			return model
		}
	}
	return ""
}

func recentCollectionLabel(items []memory.VectorDocumentRecord) string {
	for _, item := range items {
		if collection := strings.TrimSpace(item.CollectionName); collection != "" {
			return collection
		}
	}
	return ""
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
		broadcastAdminNode(renderMemorySection(deps.MemoryStore))
		broadcastAdminNode(renderEmbeddingsSection(deps.MemoryStore, deps.EmbeddingService, deps.EmbeddingInitErr))
		return
	case adminstream.VerticalEmbeddings:
		node = renderEmbeddingsSection(deps.MemoryStore, deps.EmbeddingService, deps.EmbeddingInitErr)
	default:
		return
	}
	broadcastAdminNode(node)
}

func broadcastAdminNode(node *Node) {
	if node == nil {
		return
	}
	godomws.WsHub.Broadcast <- godomws.WebsocketMessage{
		Room:    adminRoomID,
		Content: []byte(node.Render()),
	}
}
