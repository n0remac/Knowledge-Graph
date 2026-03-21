package web

import . "github.com/n0remac/GoDom/html"

func ConversationPage() *Node {
	return Html(
		Attr("data-theme", "dark"),
		pageHead("Live Conversation State", conversationPageCSS()),
		Body(
			Class("min-h-screen bg-[var(--app-bg)] text-[var(--app-fg)]"),
			Div(
				Class("flex min-h-screen flex-col"),
				Header(
					Class("border-b border-[var(--app-border-strong)] bg-[var(--app-surface)]/95 backdrop-blur"),
					Div(
						Class("mx-auto flex w-full max-w-none flex-wrap items-center gap-3 px-4 py-4 md:px-6"),
						Div(
							Class("mr-auto flex min-w-[14rem] flex-col"),
							H1(
								Class("text-2xl font-semibold tracking-tight text-[var(--app-fg)]"),
								T("Live Conversation State"),
							),
							P(
								Class("text-sm text-[var(--app-fg-muted)]"),
								T("Transcript, per-message artifacts, working state, and telemetry traces"),
							),
						),
						Button(
							Id("conversation-reload-btn"),
							Type("button"),
							Class(uiPrimaryButtonClass("sm")),
							T("Reload"),
						),
						Div(
							Id("conversation-status"),
							Class("text-sm text-[var(--app-fg-soft)]"),
							Attr("role", "status"),
							T("Loading conversations..."),
						),
					),
				),
				Main(
					Class("flex-1 px-4 pb-4 pt-3 md:px-6"),
					Div(
						Class("grid h-full gap-4 lg:grid-cols-[21rem_minmax(0,1fr)]"),
						Aside(
							Class("rounded-3xl border border-[var(--app-border-strong)] bg-[var(--app-surface)] p-4 shadow-sm backdrop-blur"),
							H2(
								Class("text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]"),
								T("Conversations"),
							),
							Div(
								Id("conversation-list"),
								Class("mt-4 flex max-h-[calc(100vh-12rem)] flex-col gap-2 overflow-y-auto pr-1"),
							),
						),
						Section(
							Id("conversation-detail"),
							Class("rounded-3xl border border-[var(--app-border-strong)] bg-[var(--app-surface)] p-4 shadow-sm backdrop-blur"),
							Div(
								Class("rounded-2xl border border-dashed border-[var(--app-border)] px-4 py-8 text-center text-sm text-[var(--app-fg-soft)]"),
								T("Select a conversation to inspect its working state."),
							),
						),
					),
				),
			),
			Script(Raw(conversationPageScript())),
		),
	)
}

func conversationPageCSS() string {
	return `
.conversation-card {
  transition: border-color 140ms ease, background-color 140ms ease, transform 140ms ease;
}

.conversation-card.is-active {
  border-color: rgba(34, 211, 238, 0.68);
  background: rgba(8, 145, 178, 0.14);
  transform: translateY(-1px);
}

.conversation-section {
  border: 1px solid var(--app-border);
  border-radius: 1.25rem;
  background: var(--app-surface-muted);
  padding: 1rem;
  box-shadow: 0 1px 0 rgba(255, 255, 255, 0.03);
}

.conversation-pre {
  white-space: pre-wrap;
  word-break: break-word;
}
`
}

func conversationPageScript() string {
	return `
var conversationState = {
  conversations: [],
  selectedId: ''
};

function conversationStatus(message, isError) {
  var el = document.getElementById('conversation-status');
  if (!el) return;
  el.textContent = message;
  el.className = isError ? 'text-sm text-red-300' : 'text-sm text-[var(--app-fg-soft)]';
}

function escapeHtml(value) {
  return String(value || '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function prettyJson(value) {
  return escapeHtml(JSON.stringify(value || {}, null, 2));
}

function section(title, body) {
  return '<section class="conversation-section">' +
    '<h3 class="text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">' + escapeHtml(title) + '</h3>' +
    '<div class="mt-3">' + body + '</div>' +
    '</section>';
}

function renderConversationList() {
  var list = document.getElementById('conversation-list');
  if (!list) return;
  list.innerHTML = '';

  if (!conversationState.conversations.length) {
    list.innerHTML = '<div class="rounded-2xl border border-dashed border-[var(--app-border)] px-4 py-8 text-center text-sm text-[var(--app-fg-soft)]">No conversations recorded yet.</div>';
    return;
  }

  conversationState.conversations.forEach(function(conversation, index) {
    var button = document.createElement('button');
    button.type = 'button';
    button.className = 'conversation-card rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3 text-left text-[var(--app-fg)] shadow-sm transition hover:-translate-y-0.5 hover:border-cyan-400/60 hover:bg-cyan-500/10';
    if ((conversationState.selectedId && conversationState.selectedId === conversation.conversation_id) || (!conversationState.selectedId && index === 0)) {
      conversationState.selectedId = conversation.conversation_id;
      button.classList.add('is-active');
    }
    button.innerHTML =
      '<div class="flex items-center justify-between gap-3">' +
        '<span class="truncate font-semibold text-[var(--app-fg)]">' + escapeHtml(conversation.conversation_id) + '</span>' +
        '<span class="rounded-full border border-cyan-400/30 bg-cyan-400/12 px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-cyan-200">' + escapeHtml(conversation.message_count) + ' msgs</span>' +
      '</div>' +
      '<p class="mt-2 text-xs text-[var(--app-fg-muted)]">' + escapeHtml((conversation.latest_message && conversation.latest_message.content) || '') + '</p>';
    button.addEventListener('click', function() {
      conversationState.selectedId = conversation.conversation_id;
      renderConversationList();
      renderConversationDetail();
    });
    list.appendChild(button);
  });
}

function renderConversationDetail() {
  var detail = document.getElementById('conversation-detail');
  if (!detail) return;
  var selected = conversationState.conversations.find(function(item) {
    return item.conversation_id === conversationState.selectedId;
  });
  if (!selected) {
    detail.innerHTML = '<div class="rounded-2xl border border-dashed border-[var(--app-border)] px-4 py-8 text-center text-sm text-[var(--app-fg-soft)]">Select a conversation to inspect its working state.</div>';
    return;
  }

  var workingState = selected.working_state || {};
  var activeTopics = (workingState.active_topics || []).map(function(topic) {
    return '<li><span class="font-medium text-[var(--app-fg)]">' + escapeHtml(topic.name) + '</span> <span class="text-[var(--app-fg-soft)]">(' + escapeHtml(topic.status) + ', ' + escapeHtml(Number(topic.salience || 0).toFixed(2)) + ')</span></li>';
  }).join('') || '<li>none</li>';

  var activeClaims = (workingState.active_claims || []).map(function(claim) {
    return '<li><span class="font-medium text-[var(--app-fg)]">' + escapeHtml(claim.subject + ' | ' + claim.predicate + ' | ' + claim.object) + '</span> <span class="text-[var(--app-fg-soft)]">(' + escapeHtml(Number(claim.salience || 0).toFixed(2)) + ')</span></li>';
  }).join('') || '<li>none</li>';

  var recentMessages = (selected.recent_messages || []).map(function(message) {
    return '<div class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] px-4 py-3">' +
      '<div class="flex flex-wrap items-center gap-2 text-xs uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">' +
        '<span>' + escapeHtml(message.author_role) + '</span>' +
        '<span>' + escapeHtml(message.author_id) + '</span>' +
        '<span>#' + escapeHtml(message.sequence_number) + '</span>' +
      '</div>' +
      '<p class="mt-2 text-sm text-[var(--app-fg)]">' + escapeHtml(message.content) + '</p>' +
    '</div>';
  }).join('') || '<div class="text-sm text-[var(--app-fg-soft)]">No recent messages.</div>';

  var recentExtractions = (selected.recent_extractions || []).map(function(extraction) {
    return '<details class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] px-4 py-3">' +
      '<summary class="cursor-pointer font-medium text-[var(--app-fg)]">' + escapeHtml(extraction.message_id) + ' <span class="text-xs text-[var(--app-fg-soft)]">claims=' + escapeHtml(extraction.claims_status) + ', topics=' + escapeHtml(extraction.topics_status) + ', summary=' + escapeHtml(extraction.summary_status) + '</span></summary>' +
      '<div class="mt-3 grid gap-3 lg:grid-cols-2">' +
        '<pre class="conversation-pre rounded-xl border border-slate-800 bg-slate-950/90 p-3 text-xs text-slate-100">' + prettyJson(extraction) + '</pre>' +
        '<div class="space-y-3">' +
          '<div><p class="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">Raw Claims Output</p><pre class="conversation-pre mt-2 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface)] p-3 text-xs text-[var(--app-fg)]">' + escapeHtml(extraction.raw_claims_output || '') + '</pre></div>' +
          '<div><p class="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">Raw Topics Output</p><pre class="conversation-pre mt-2 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface)] p-3 text-xs text-[var(--app-fg)]">' + escapeHtml(extraction.raw_topics_output || '') + '</pre></div>' +
          '<div><p class="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">Raw Summary Output</p><pre class="conversation-pre mt-2 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface)] p-3 text-xs text-[var(--app-fg)]">' + escapeHtml(extraction.raw_summary_output || '') + '</pre></div>' +
        '</div>' +
      '</div>' +
    '</details>';
  }).join('') || '<div class="text-sm text-[var(--app-fg-soft)]">No recent extractions.</div>';

  var latestTrace = selected.latest_trace || {};
  var traceEvents = (latestTrace.events || []).map(function(event) {
    return '<details class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] px-4 py-3">' +
      '<summary class="cursor-pointer font-medium text-[var(--app-fg)]">' + escapeHtml(event.name) + '</summary>' +
      '<pre class="conversation-pre mt-3 rounded-xl border border-slate-800 bg-slate-950/90 p-3 text-xs text-slate-100">' + escapeHtml(event.content || '') + '</pre>' +
    '</details>';
  }).join('') || '<div class="text-sm text-[var(--app-fg-soft)]">No trace artifacts available for the latest message.</div>';

  detail.innerHTML =
    '<div class="space-y-4">' +
      '<div class="rounded-3xl border border-[var(--app-border-strong)] bg-[var(--app-surface-muted)] px-5 py-4">' +
        '<div class="flex flex-wrap items-center gap-3">' +
          '<h2 class="text-xl font-semibold text-[var(--app-fg)]">' + escapeHtml(selected.conversation_id) + '</h2>' +
          '<span class="rounded-full border border-cyan-400/30 bg-cyan-400/12 px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-cyan-200">' + escapeHtml(selected.message_count) + ' messages</span>' +
        '</div>' +
        '<p class="mt-2 text-sm text-[var(--app-fg-muted)]">Latest message: ' + escapeHtml((selected.latest_message && selected.latest_message.content) || '') + '</p>' +
      '</div>' +
      '<div class="grid gap-4 xl:grid-cols-2">' +
        section('Rolling Summary', '<p class="text-sm leading-7 text-[var(--app-fg)]">' + escapeHtml(workingState.rolling_summary || 'No rolling summary yet.') + '</p>') +
        section('Response Context Brief', '<pre class="conversation-pre rounded-xl border border-slate-800 bg-slate-950/90 p-4 text-xs text-slate-100">' + escapeHtml((selected.latest_response_context && selected.latest_response_context.brief) || 'No response context artifact yet.') + '</pre>') +
        section('Active Topics', '<ul class="space-y-2 text-sm text-[var(--app-fg-muted)]">' + activeTopics + '</ul>') +
        section('Active Claims', '<ul class="space-y-2 text-sm text-[var(--app-fg-muted)]">' + activeClaims + '</ul>') +
      '</div>' +
      section('Recent Exchange', '<div class="space-y-3">' + recentMessages + '</div>') +
      section('Recent Message Extractions', '<div class="space-y-3">' + recentExtractions + '</div>') +
      section('Latest Trace Summary', '<div class="grid gap-3 xl:grid-cols-2"><pre class="conversation-pre rounded-xl border border-slate-800 bg-slate-950/90 p-4 text-xs text-slate-100">' + prettyJson(latestTrace.summary || {}) + '</pre><pre class="conversation-pre rounded-xl border border-[var(--app-border)] bg-[var(--app-surface)] p-4 text-xs text-[var(--app-fg)]">' + prettyJson(latestTrace.index || {}) + '</pre></div>') +
      section('Raw Trace Artifacts', '<div class="space-y-3">' + traceEvents + '</div>') +
    '</div>';
}

function loadConversationData() {
  conversationStatus('Loading conversations...', false);
  fetch('/conversation/data', { headers: { Accept: 'application/json' } })
    .then(function(response) {
      if (!response.ok) {
        throw new Error('Request failed with status ' + response.status);
      }
      return response.json();
    })
    .then(function(data) {
      conversationState.conversations = Array.isArray(data.conversations) ? data.conversations : [];
      if (!conversationState.conversations.some(function(item) { return item.conversation_id === conversationState.selectedId; })) {
        conversationState.selectedId = conversationState.conversations.length ? conversationState.conversations[0].conversation_id : '';
      }
      renderConversationList();
      renderConversationDetail();
      conversationStatus('Loaded ' + conversationState.conversations.length + ' conversation(s).', false);
    })
    .catch(function(error) {
      conversationStatus(error.message || 'Failed to load conversations.', true);
    });
}

document.addEventListener('DOMContentLoaded', function() {
  var reloadButton = document.getElementById('conversation-reload-btn');
  if (reloadButton) {
    reloadButton.addEventListener('click', loadConversationData);
  }
  loadConversationData();
});
`
}
