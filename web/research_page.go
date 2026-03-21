package web

import . "github.com/n0remac/GoDom/html"

func ResearchPage() *Node {
	return Html(
		Attr("data-theme", "dark"),
		pageHead("Research Test Slice", researchPageCSS()),
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
							H1(Class("text-2xl font-semibold tracking-tight text-[var(--app-fg)]"), T("Research Test Slice")),
							P(Class("text-sm text-[var(--app-fg-muted)]"), T("Run memory-only research queries, inspect normalized artifacts, and review the rendered context preview.")),
						),
						Button(
							Id("research-reload-btn"),
							Type("button"),
							Class(uiPrimaryButtonClass("sm")),
							T("Reload"),
						),
						Div(
							Id("research-status"),
							Class("text-sm text-[var(--app-fg-soft)]"),
							Attr("role", "status"),
							T("Loading research runs..."),
						),
					),
				),
				Main(
					Class("flex-1 px-4 pb-4 pt-3 md:px-6"),
					Div(
						Class("grid gap-4 xl:grid-cols-[24rem_minmax(0,1fr)]"),
						Aside(
							Class("rounded-3xl border border-[var(--app-border-strong)] bg-[var(--app-surface)] p-4 shadow-sm backdrop-blur"),
							Section(
								Class("rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4"),
								H2(Class("text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]"), T("Run Research")),
								Div(
									Class("mt-4 space-y-4"),
									Div(
										Label(For("research-query"), Class(uiLabelClass()), T("Query")),
										TextArea(Id("research-query"), Class(uiTextareaClass()), Attr("placeholder", "Find alpha memory traces")),
									),
									Div(
										Label(For("research-conversation-id"), Class(uiLabelClass()), T("Conversation Filter")),
										Input(Id("research-conversation-id"), Class(uiInputClass()), Attr("placeholder", "Optional channel or conversation id")),
									),
									Div(
										Label(For("research-top-k"), Class(uiLabelClass()), T("Top K")),
										Input(Id("research-top-k"), Type("number"), Attr("min", "1"), Max("20"), Value("8"), Class(uiInputClass())),
									),
									Div(
										Class("rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] px-4 py-3 text-sm text-[var(--app-fg-muted)]"),
										Span(Class("font-semibold text-[var(--app-fg)]"), T("Embedding Model: ")),
										Span(Id("research-default-model"), T("Loading...")),
									),
									Button(Id("research-run-btn"), Type("button"), Class(uiPrimaryButtonClass("sm")+" w-full"), T("Run Query")),
								),
							),
							Section(
								Class("mt-4 rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4"),
								Div(
									Class("flex items-center justify-between gap-3"),
									H2(Class("text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]"), T("Recent Runs")),
									Span(Id("research-run-count"), Class("text-xs text-[var(--app-fg-soft)]"), T("0")),
								),
								Div(Id("research-run-list"), Class("mt-4 flex max-h-[calc(100vh-22rem)] flex-col gap-2 overflow-y-auto pr-1")),
							),
						),
						Section(
							Class("rounded-3xl border border-[var(--app-border-strong)] bg-[var(--app-surface)] p-4 shadow-sm backdrop-blur"),
							Div(
								Class("flex items-center justify-between gap-3"),
								H2(Class("text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]"), T("Run Detail")),
								Span(Id("research-selected-run"), Class("text-xs text-[var(--app-fg-soft)]"), T("No run selected")),
							),
							Div(Id("research-run-detail"), Class("mt-4")),
						),
					),
				),
			),
			Script(Raw(researchPageScript())),
		),
	)
}

func researchPageCSS() string {
	return `
.research-card {
  transition: border-color 140ms ease, background-color 140ms ease, transform 140ms ease;
}

.research-card.is-active {
  border-color: rgba(34, 197, 94, 0.68);
  background: rgba(34, 197, 94, 0.12);
  transform: translateY(-1px);
}

.research-block {
  border: 1px solid var(--app-border);
  border-radius: 1.25rem;
  background: var(--app-surface-muted);
  padding: 1rem;
  box-shadow: 0 1px 0 rgba(255, 255, 255, 0.03);
}

.research-code {
  white-space: pre-wrap;
  word-break: break-word;
}
`
}

func researchPageScript() string {
	return `
var researchState = {
  defaults: { embedding_model: '' },
  runs: [],
  selectedRunId: '',
  selectedRun: null
};

function researchStatus(message, isError) {
  var el = document.getElementById('research-status');
  if (!el) return;
  el.textContent = message;
  el.className = isError ? 'text-sm text-red-300' : 'text-sm text-[var(--app-fg-soft)]';
}

function researchEscapeHtml(value) {
  return String(value || '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function researchRenderRuns() {
  var list = document.getElementById('research-run-list');
  var count = document.getElementById('research-run-count');
  if (!list || !count) return;
  count.textContent = String(researchState.runs.length);
  list.innerHTML = '';

  if (!researchState.runs.length) {
    list.innerHTML = '<div class="rounded-2xl border border-dashed border-[var(--app-border)] px-4 py-8 text-center text-sm text-[var(--app-fg-soft)]">No research runs yet.</div>';
    return;
  }

  researchState.runs.forEach(function(run) {
    var button = document.createElement('button');
    button.type = 'button';
    button.className = 'research-card rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3 text-left shadow-sm transition hover:-translate-y-0.5 hover:border-cyan-400/60 hover:bg-cyan-500/10';
    if (run.id === researchState.selectedRunId) {
      button.classList.add('is-active');
    }
    button.innerHTML =
      '<div class="flex items-center justify-between gap-3">' +
        '<span class="truncate font-semibold text-[var(--app-fg)]">' + researchEscapeHtml(run.query) + '</span>' +
        '<span class="rounded-full border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-2 py-1 text-[0.68rem] uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">' + researchEscapeHtml(run.status) + '</span>' +
      '</div>' +
      '<p class="mt-2 text-xs text-[var(--app-fg-muted)]">scope: ' + researchEscapeHtml(run.conversation_id || 'global') + ' · artifacts: ' + researchEscapeHtml(run.artifact_count || 0) + '</p>';
    button.addEventListener('click', function() {
      researchLoadRun(run.id);
    });
    list.appendChild(button);
  });
}

function researchDetailSection(title, body) {
  return '<section class="research-block">' +
    '<h3 class="text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">' + researchEscapeHtml(title) + '</h3>' +
    '<div class="mt-3">' + body + '</div>' +
  '</section>';
}

function researchRenderDetail() {
  var detail = document.getElementById('research-run-detail');
  var selected = document.getElementById('research-selected-run');
  if (!detail || !selected) return;

  if (!researchState.selectedRun) {
    selected.textContent = 'No run selected';
    detail.innerHTML = '<div class="rounded-2xl border border-dashed border-[var(--app-border)] px-6 py-16 text-center text-sm text-[var(--app-fg-soft)]">Run a query or select a previous run to inspect artifacts and provenance.</div>';
    return;
  }

  var run = researchState.selectedRun.run;
  var artifacts = Array.isArray(researchState.selectedRun.artifacts) ? researchState.selectedRun.artifacts : [];
  selected.textContent = run.id;

  var evidenceBody = '<div class="space-y-3">';
  if (run.context && Array.isArray(run.context.evidence) && run.context.evidence.length) {
    run.context.evidence.forEach(function(item) {
      evidenceBody += '<article class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] px-4 py-3">' +
        '<div class="flex items-center justify-between gap-3 text-xs uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">' +
          '<span>' + researchEscapeHtml(item.kind) + '</span>' +
          '<span>score ' + researchEscapeHtml(Number(item.score || 0).toFixed(3)) + '</span>' +
        '</div>' +
        '<p class="mt-2 text-sm text-[var(--app-fg)]">' + researchEscapeHtml(item.snippet) + '</p>' +
      '</article>';
    });
  } else {
    evidenceBody += '<p class="text-sm text-[var(--app-fg-soft)]">No evidence selected.</p>';
  }
  evidenceBody += '</div>';

  var artifactBody = '<div class="space-y-3">';
  if (artifacts.length) {
    artifacts.forEach(function(artifact) {
      var provenance = artifact.provenance || {};
      artifactBody += '<article class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] px-4 py-3">' +
        '<div class="flex flex-wrap items-center gap-2 text-xs uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">' +
          '<span>' + researchEscapeHtml(artifact.kind) + '</span>' +
          '<span>source ' + researchEscapeHtml(artifact.source_name) + '</span>' +
          '<span>rank ' + researchEscapeHtml(provenance.search_rank || 0) + '</span>' +
          '<span>score ' + researchEscapeHtml(Number(provenance.search_score || 0).toFixed(3)) + '</span>' +
        '</div>' +
        '<h4 class="mt-2 text-base font-semibold text-[var(--app-fg)]">' + researchEscapeHtml(artifact.title) + '</h4>' +
        '<p class="mt-2 text-sm text-[var(--app-fg-muted)]">' + researchEscapeHtml(artifact.content) + '</p>' +
        '<dl class="mt-3 grid gap-2 text-xs text-[var(--app-fg-soft)] md:grid-cols-2">' +
          '<div><dt class="font-semibold text-[var(--app-fg-muted)]">Conversation</dt><dd>' + researchEscapeHtml(artifact.conversation_id || 'global') + '</dd></div>' +
          '<div><dt class="font-semibold text-[var(--app-fg-muted)]">Author</dt><dd>' + researchEscapeHtml(artifact.author_id || 'unknown') + '</dd></div>' +
          '<div><dt class="font-semibold text-[var(--app-fg-muted)]">Source ID</dt><dd>' + researchEscapeHtml(artifact.source_id) + '</dd></div>' +
          '<div><dt class="font-semibold text-[var(--app-fg-muted)]">Selection Reason</dt><dd>' + researchEscapeHtml(provenance.selection_reason || 'retrieved') + '</dd></div>' +
        '</dl>' +
      '</article>';
    });
  } else {
    artifactBody += '<p class="text-sm text-[var(--app-fg-soft)]">No stored artifacts.</p>';
  }
  artifactBody += '</div>';

  var summaryBody =
    '<dl class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">' +
      '<div class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] px-4 py-3"><dt class="text-xs uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">Scope</dt><dd class="mt-2 text-sm font-semibold text-[var(--app-fg)]">' + researchEscapeHtml(run.conversation_id || 'global') + '</dd></div>' +
      '<div class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] px-4 py-3"><dt class="text-xs uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">Top K</dt><dd class="mt-2 text-sm font-semibold text-[var(--app-fg)]">' + researchEscapeHtml(run.top_k) + '</dd></div>' +
      '<div class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] px-4 py-3"><dt class="text-xs uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">Artifacts</dt><dd class="mt-2 text-sm font-semibold text-[var(--app-fg)]">' + researchEscapeHtml(run.artifact_count) + '</dd></div>' +
      '<div class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] px-4 py-3"><dt class="text-xs uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">Model</dt><dd class="mt-2 text-sm font-semibold text-[var(--app-fg)]">' + researchEscapeHtml(run.embedding_model) + '</dd></div>' +
    '</dl>';

  var brief = (run.context && run.context.rendered_brief) ? run.context.rendered_brief : 'No rendered brief.';

  detail.innerHTML =
    '<div class="space-y-4">' +
      researchDetailSection('Run Summary', summaryBody) +
      researchDetailSection('Rendered Context Preview', '<pre class="research-code rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] px-4 py-3 text-sm text-[var(--app-fg)]">' + researchEscapeHtml(brief) + '</pre>') +
      researchDetailSection('Evidence', evidenceBody) +
      researchDetailSection('Artifacts', artifactBody) +
    '</div>';
}

async function researchLoadData() {
  researchStatus('Loading research runs...', false);
  try {
    var response = await fetch('/research/data', { cache: 'no-store' });
    if (!response.ok) {
      throw new Error(await response.text() || 'Failed to load research data');
    }
    var data = await response.json();
    researchState.defaults = data.defaults || { embedding_model: '' };
    researchState.runs = Array.isArray(data.runs) ? data.runs : [];
    var defaultModel = document.getElementById('research-default-model');
    if (defaultModel) {
      defaultModel.textContent = researchState.defaults.embedding_model || 'Unknown';
    }
    researchRenderRuns();
    if (researchState.selectedRunId) {
      var stillExists = researchState.runs.some(function(run) { return run.id === researchState.selectedRunId; });
      if (!stillExists) {
        researchState.selectedRunId = '';
        researchState.selectedRun = null;
      }
    }
    researchRenderDetail();
    researchStatus('Research runs loaded.', false);
  } catch (error) {
    researchStatus(error.message || 'Failed to load research data.', true);
  }
}

async function researchLoadRun(runId) {
  if (!runId) return;
  researchStatus('Loading run ' + runId + '...', false);
  try {
    var response = await fetch('/research/runs/' + encodeURIComponent(runId), { cache: 'no-store' });
    if (!response.ok) {
      throw new Error(await response.text() || 'Failed to load research run');
    }
    researchState.selectedRun = await response.json();
    researchState.selectedRunId = runId;
    researchRenderRuns();
    researchRenderDetail();
    researchStatus('Loaded run ' + runId + '.', false);
  } catch (error) {
    researchStatus(error.message || 'Failed to load research run.', true);
  }
}

async function researchRunQuery() {
  var queryEl = document.getElementById('research-query');
  var conversationEl = document.getElementById('research-conversation-id');
  var topKEl = document.getElementById('research-top-k');
  if (!queryEl || !conversationEl || !topKEl) return;

  researchStatus('Running research query...', false);
  try {
    var response = await fetch('/research/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        query: queryEl.value,
        conversation_id: conversationEl.value,
        top_k: Number(topKEl.value || 0)
      })
    });
    if (!response.ok) {
      throw new Error(await response.text() || 'Failed to run research query');
    }
    var detail = await response.json();
    researchState.selectedRun = detail;
    researchState.selectedRunId = detail.run ? detail.run.id : '';
    researchState.runs = [detail.run].concat(researchState.runs.filter(function(run) {
      return detail.run && run.id !== detail.run.id;
    }));
    researchRenderRuns();
    researchRenderDetail();
    researchStatus('Research query completed.', false);
  } catch (error) {
    researchStatus(error.message || 'Failed to run research query.', true);
  }
}

document.addEventListener('DOMContentLoaded', function() {
  var reloadBtn = document.getElementById('research-reload-btn');
  if (reloadBtn) {
    reloadBtn.addEventListener('click', function() {
      researchLoadData();
    });
  }

  var runBtn = document.getElementById('research-run-btn');
  if (runBtn) {
    runBtn.addEventListener('click', function() {
      researchRunQuery();
    });
  }

  researchLoadData();
});
`
}
