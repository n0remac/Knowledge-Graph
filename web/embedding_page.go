package web

import . "github.com/n0remac/GoDom/html"

func EmbeddingPage() *Node {
	return Html(
		Attr("data-theme", "dark"),
		pageHead("Embedding Experiments", embeddingPageCSS()),
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
							H1(Class("text-2xl font-semibold tracking-tight text-[var(--app-fg)]"), T("Embedding Experiments")),
							P(Class("text-sm text-[var(--app-fg-muted)]"), T("Create message sets, run retrieval queries, and inspect ranked results.")),
						),
						Button(
							Id("embeddings-reload-btn"),
							Type("button"),
							Class(uiPrimaryButtonClass("sm")),
							T("Reload"),
						),
						Div(
							Id("embeddings-status"),
							Class("text-sm text-[var(--app-fg-soft)]"),
							Attr("role", "status"),
							T("Loading message sets..."),
						),
					),
				),
				Main(
					Class("flex-1 px-4 pb-4 pt-3 md:px-6"),
					Div(
						Class("grid gap-4 xl:grid-cols-[18rem_minmax(0,1fr)_minmax(0,1.1fr)]"),
						Aside(
							Class("rounded-3xl border border-[var(--app-border-strong)] bg-[var(--app-surface)] p-4 shadow-sm backdrop-blur"),
							Div(
								Class("flex items-center justify-between gap-3"),
								H2(Class("text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]"), T("Message Sets")),
								Button(Id("embeddings-new-set-btn"), Type("button"), Class(uiPrimaryButtonClass("xs")), T("New")),
							),
							Div(Id("embeddings-set-list"), Class("mt-4 flex max-h-[calc(100vh-12rem)] flex-col gap-2 overflow-y-auto pr-1")),
						),
						Section(
							Class("rounded-3xl border border-[var(--app-border-strong)] bg-[var(--app-surface)] p-4 shadow-sm backdrop-blur"),
							Div(
								Class("flex items-center justify-between gap-3"),
								H2(Class("text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]"), T("Message Set Editor")),
								Div(Class("flex items-center gap-2"),
									Button(Id("embeddings-delete-set-btn"), Type("button"), Class(uiDangerButtonClass("sm")), T("Delete")),
									Button(Id("embeddings-save-set-btn"), Type("button"), Class(uiPrimaryButtonClass("sm")), T("Save")),
								),
							),
							Div(Id("embeddings-set-editor"), Class("mt-4")),
						),
						Section(
							Class("rounded-3xl border border-[var(--app-border-strong)] bg-[var(--app-surface)] p-4 shadow-sm backdrop-blur"),
							Div(
								Class("flex items-center justify-between gap-3"),
								H2(Class("text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]"), T("Retrieval Runs")),
								Button(Id("embeddings-run-btn"), Type("button"), Class(uiPrimaryButtonClass("sm")), T("Run Query")),
							),
							Div(Id("embeddings-run-panel"), Class("mt-4 space-y-4")),
						),
					),
				),
			),
			Script(Raw(embeddingPageScript())),
		),
	)
}

func embeddingPageCSS() string {
	return `
.embedding-card {
  transition: border-color 140ms ease, background-color 140ms ease, transform 140ms ease;
}

.embedding-card.is-active {
  border-color: rgba(16, 185, 129, 0.68);
  background: rgba(16, 185, 129, 0.12);
  transform: translateY(-1px);
}

.embedding-section {
  border: 1px solid var(--app-border);
  border-radius: 1.25rem;
  background: var(--app-surface-muted);
  padding: 1rem;
  box-shadow: 0 1px 0 rgba(255, 255, 255, 0.03);
}

.embedding-label {
  display: block;
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--app-fg-soft);
}
`
}

func embeddingPageScript() string {
	return `
var embeddingState = {
  defaults: { embedding_model: '' },
  models: [],
  messageSets: [],
  runs: [],
  editorSet: null,
  selectedSetId: '',
  selectedRunId: '',
  selectedRun: null
};

var embeddingUi = {
  input: 'w-full rounded-2xl border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-3 py-2.5 text-sm text-[var(--app-fg)] shadow-sm outline-none transition placeholder:text-[var(--app-fg-soft)] focus:border-emerald-400/70 focus:ring-4 focus:ring-emerald-400/15',
  inputCompact: 'rounded-2xl border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-3 py-2.5 text-sm text-[var(--app-fg)] shadow-sm outline-none transition placeholder:text-[var(--app-fg-soft)] focus:border-emerald-400/70 focus:ring-4 focus:ring-emerald-400/15',
  textarea: 'w-full min-h-24 resize-y rounded-2xl border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-3 py-2.5 text-sm text-[var(--app-fg)] shadow-sm outline-none transition placeholder:text-[var(--app-fg-soft)] focus:border-emerald-400/70 focus:ring-4 focus:ring-emerald-400/15',
  select: 'w-full rounded-2xl border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-3 py-2.5 pr-10 text-sm text-[var(--app-fg)] shadow-sm outline-none transition focus:border-emerald-400/70 focus:ring-4 focus:ring-emerald-400/15',
  primaryXs: 'inline-flex h-8 items-center justify-center rounded-xl border border-emerald-300/30 bg-emerald-400 px-3 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-slate-950 shadow-sm transition duration-150 hover:-translate-y-0.5 hover:bg-emerald-300 focus:outline-none focus:ring-4 focus:ring-emerald-400/25',
  secondaryXs: 'inline-flex h-8 items-center justify-center rounded-xl border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-3 text-[0.68rem] font-medium uppercase tracking-[0.18em] text-[var(--app-fg)] shadow-sm transition duration-150 hover:-translate-y-0.5 hover:border-emerald-400/50 hover:bg-emerald-500/10 focus:outline-none focus:ring-4 focus:ring-emerald-400/15',
  dangerXs: 'inline-flex h-8 items-center justify-center rounded-xl border border-red-500/35 bg-red-500/10 px-3 text-[0.68rem] font-medium uppercase tracking-[0.18em] text-red-300 shadow-sm transition duration-150 hover:-translate-y-0.5 hover:bg-red-500/16 focus:outline-none focus:ring-4 focus:ring-red-500/15'
};

function embeddingStatus(message, isError) {
  var el = document.getElementById('embeddings-status');
  if (!el) return;
  el.textContent = message;
  el.className = isError ? 'text-sm text-red-300' : 'text-sm text-[var(--app-fg-soft)]';
}

function embeddingEscapeHtml(value) {
  return String(value || '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function embeddingClone(value) {
  return JSON.parse(JSON.stringify(value));
}

function emptyMessageSet() {
  return {
    id: '',
    name: '',
    description: '',
    messages: [{ text: '' }]
  };
}

function selectedMessageSet() {
  return embeddingState.editorSet || emptyMessageSet();
}

function embeddingSection(title, body) {
  return '<section class="embedding-section">' +
    '<h3 class="text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">' + embeddingEscapeHtml(title) + '</h3>' +
    '<div class="mt-3">' + body + '</div>' +
    '</section>';
}

function embeddingSelectOptions(models, selectedValue) {
  var list = Array.isArray(models) ? models.slice() : [];
  var selected = String(selectedValue || '').trim();
  if (selected && !list.some(function(item) { return item && item.name === selected; })) {
    list = [{ name: selected }].concat(list);
  }
  return list.map(function(item) {
    var name = item && item.name ? String(item.name) : '';
    if (!name) return '';
    var selectedAttr = name === selected ? ' selected' : '';
    return '<option value="' + embeddingEscapeHtml(name) + '"' + selectedAttr + '>' + embeddingEscapeHtml(name) + '</option>';
  }).join('');
}

function renderMessageSetList() {
  var list = document.getElementById('embeddings-set-list');
  if (!list) return;
  list.innerHTML = '';

  if (!embeddingState.messageSets.length) {
    list.innerHTML = '<div class="rounded-2xl border border-dashed border-[var(--app-border)] px-4 py-8 text-center text-sm text-[var(--app-fg-soft)]">No saved message sets yet.</div>';
    return;
  }

  embeddingState.messageSets.forEach(function(messageSet) {
    var button = document.createElement('button');
    button.type = 'button';
    button.className = 'embedding-card rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3 text-left text-[var(--app-fg)] shadow-sm transition hover:-translate-y-0.5 hover:border-emerald-400/60 hover:bg-emerald-500/10';
    if (messageSet.id === embeddingState.selectedSetId) {
      button.classList.add('is-active');
    }
    button.innerHTML =
      '<div class="flex items-center justify-between gap-3">' +
        '<span class="truncate font-semibold text-[var(--app-fg)]">' + embeddingEscapeHtml(messageSet.name) + '</span>' +
        '<span class="rounded-full border border-emerald-400/30 bg-emerald-400/12 px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-emerald-200">' + embeddingEscapeHtml(messageSet.messages.length) + ' msgs</span>' +
      '</div>' +
      '<p class="mt-2 text-xs text-[var(--app-fg-muted)]">' + embeddingEscapeHtml(messageSet.description || 'No description.') + '</p>';
    button.addEventListener('click', function() {
      embeddingState.selectedSetId = messageSet.id;
      embeddingState.editorSet = embeddingClone(messageSet);
      renderMessageSetList();
      renderMessageSetEditor();
      renderRunPanel();
    });
    list.appendChild(button);
  });
}

function renderMessageSetEditor() {
  var editor = document.getElementById('embeddings-set-editor');
  if (!editor) return;
  var messageSet = selectedMessageSet();

  editor.innerHTML =
    '<div class="space-y-4">' +
      '<div>' +
        '<label class="embedding-label" for="embeddings-set-name">Name</label>' +
        '<input id="embeddings-set-name" class="mt-2 ' + embeddingUi.input + '" value="' + embeddingEscapeHtml(messageSet.name || '') + '" />' +
      '</div>' +
      '<div>' +
        '<label class="embedding-label" for="embeddings-set-description">Description</label>' +
        '<textarea id="embeddings-set-description" class="mt-2 ' + embeddingUi.textarea + '">' + embeddingEscapeHtml(messageSet.description || '') + '</textarea>' +
      '</div>' +
      '<div class="flex items-center justify-between gap-3">' +
        '<span class="embedding-label">Messages</span>' +
        '<button id="embeddings-add-message-btn" type="button" class="' + embeddingUi.primaryXs + '">Add Message</button>' +
      '</div>' +
      '<div id="embeddings-message-list" class="space-y-3"></div>' +
    '</div>';

  var messageList = document.getElementById('embeddings-message-list');
  (messageSet.messages || []).forEach(function(message, index) {
    var node = document.createElement('div');
    node.className = 'rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-3';
    node.innerHTML =
      '<div class="flex items-center justify-between gap-3">' +
        '<span class="embedding-label">Message ' + embeddingEscapeHtml(index + 1) + '</span>' +
        '<div class="flex items-center gap-2">' +
          '<button data-action="up" data-index="' + embeddingEscapeHtml(index) + '" type="button" class="' + embeddingUi.secondaryXs + '">Up</button>' +
          '<button data-action="down" data-index="' + embeddingEscapeHtml(index) + '" type="button" class="' + embeddingUi.secondaryXs + '">Down</button>' +
          '<button data-action="delete" data-index="' + embeddingEscapeHtml(index) + '" type="button" class="' + embeddingUi.dangerXs + '">Delete</button>' +
        '</div>' +
      '</div>' +
      '<textarea data-message-index="' + embeddingEscapeHtml(index) + '" class="mt-2 ' + embeddingUi.textarea + '">' + embeddingEscapeHtml(message.text || '') + '</textarea>';
    messageList.appendChild(node);
  });

  editor.querySelectorAll('[data-message-index]').forEach(function(node) {
    node.addEventListener('input', function(event) {
      var index = Number(event.target.getAttribute('data-message-index'));
      embeddingState.editorSet.messages[index].text = event.target.value;
    });
  });

  editor.querySelectorAll('[data-action]').forEach(function(node) {
    node.addEventListener('click', function(event) {
      var index = Number(event.target.getAttribute('data-index'));
      var action = event.target.getAttribute('data-action');
      if (action === 'delete') {
        embeddingState.editorSet.messages.splice(index, 1);
        if (!embeddingState.editorSet.messages.length) {
          embeddingState.editorSet.messages.push({ text: '' });
        }
      } else if (action === 'up' && index > 0) {
        var prev = embeddingState.editorSet.messages[index - 1];
        embeddingState.editorSet.messages[index - 1] = embeddingState.editorSet.messages[index];
        embeddingState.editorSet.messages[index] = prev;
      } else if (action === 'down' && index < embeddingState.editorSet.messages.length - 1) {
        var next = embeddingState.editorSet.messages[index + 1];
        embeddingState.editorSet.messages[index + 1] = embeddingState.editorSet.messages[index];
        embeddingState.editorSet.messages[index] = next;
      }
      renderMessageSetEditor();
    });
  });

  document.getElementById('embeddings-set-name').addEventListener('input', function(event) {
    embeddingState.editorSet.name = event.target.value;
  });
  document.getElementById('embeddings-set-description').addEventListener('input', function(event) {
    embeddingState.editorSet.description = event.target.value;
  });
  document.getElementById('embeddings-add-message-btn').addEventListener('click', function() {
    embeddingState.editorSet.messages.push({ text: '' });
    renderMessageSetEditor();
  });
}

function renderRunPanel() {
  var panel = document.getElementById('embeddings-run-panel');
  if (!panel) return;
  var messageSet = selectedMessageSet();
  var selectedRun = embeddingState.selectedRun;
  var defaultModel = (embeddingState.defaults && embeddingState.defaults.embedding_model) || '';
  var modelOptions = embeddingSelectOptions(embeddingState.models || [], defaultModel);

  panel.innerHTML =
    '<div class="space-y-4">' +
      embeddingSection('Run Query',
        '<div class="space-y-3">' +
          '<div><label class="embedding-label" for="embeddings-model">Embedding Model</label><select id="embeddings-model" class="mt-2 ' + embeddingUi.select + '">' + modelOptions + '</select></div>' +
          '<div><label class="embedding-label" for="embeddings-query">Query</label><textarea id="embeddings-query" class="mt-2 ' + embeddingUi.textarea + '" placeholder="Find the message about..."></textarea></div>' +
          '<div><label class="embedding-label" for="embeddings-topk">Top K</label><input id="embeddings-topk" type="number" min="1" max="20" value="5" class="mt-2 w-32 ' + embeddingUi.inputCompact + '" /></div>' +
          '<p class="text-xs text-[var(--app-fg-soft)]">Selected message set: ' + embeddingEscapeHtml(messageSet.name || 'Unsaved draft') + '</p>' +
        '</div>') +
      embeddingSection('Latest Result',
        selectedRun ? renderRunResults(selectedRun) : '<p class="text-sm text-[var(--app-fg-soft)]">Run a query or select a previous run to inspect ranked results.</p>') +
      embeddingSection('Recent Runs', renderRunList()) +
    '</div>';
}

function renderRunResults(run) {
  var results = run && Array.isArray(run.results) ? run.results : [];
  if (!results.length) {
    return '<p class="text-sm text-[var(--app-fg-soft)]">' + embeddingEscapeHtml(run.error || 'No results recorded.') + '</p>';
  }
  return '<div class="space-y-3">' +
    '<div class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-3 text-sm text-[var(--app-fg-muted)]">Model: <strong class="text-[var(--app-fg)]">' + embeddingEscapeHtml(run.embedding_model) + '</strong><br/>Collection: <strong class="text-[var(--app-fg)]">' + embeddingEscapeHtml(run.collection_name || 'n/a') + '</strong><br/>Query: ' + embeddingEscapeHtml(run.query || '') + '</div>' +
    results.map(function(result) {
      return '<div class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-3">' +
        '<div class="flex items-center justify-between gap-3 text-xs uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">' +
          '<span>Rank ' + embeddingEscapeHtml(result.rank) + '</span>' +
          '<span>Score ' + embeddingEscapeHtml(Number(result.score || 0).toFixed(4)) + '</span>' +
        '</div>' +
        '<p class="mt-2 text-sm font-semibold text-[var(--app-fg)]">Message ' + embeddingEscapeHtml(result.message_index + 1) + '</p>' +
        '<p class="mt-1 text-sm text-[var(--app-fg-muted)]">' + embeddingEscapeHtml(result.message_text || '') + '</p>' +
      '</div>';
    }).join('') +
  '</div>';
}

function renderRunList() {
  if (!embeddingState.runs.length) {
    return '<div class="rounded-2xl border border-dashed border-[var(--app-border)] px-4 py-8 text-center text-sm text-[var(--app-fg-soft)]">No runs yet.</div>';
  }
  return embeddingState.runs.map(function(run) {
    var activeClass = run.id === embeddingState.selectedRunId ? ' border-emerald-400/60 bg-emerald-500/10' : ' border-[var(--app-border)] bg-[var(--app-surface-muted)]';
    return '<button data-run-id="' + embeddingEscapeHtml(run.id) + '" type="button" class="w-full rounded-2xl border px-4 py-3 text-left' + activeClass + '">' +
      '<div class="flex items-center justify-between gap-3">' +
        '<span class="font-semibold text-[var(--app-fg)]">' + embeddingEscapeHtml(run.message_set_name || run.message_set_id) + '</span>' +
        '<span class="text-xs uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">' + embeddingEscapeHtml(run.status) + '</span>' +
      '</div>' +
      '<p class="mt-2 text-xs text-[var(--app-fg-muted)]">' + embeddingEscapeHtml(run.query || '') + '</p>' +
    '</button>';
  }).join('');
}

async function refreshEmbeddingData() {
  embeddingStatus('Loading message sets...', false);
  try {
    var response = await fetch('/embeddings/data', { cache: 'no-store' });
    if (!response.ok) {
      throw new Error(await response.text() || ('request failed with status ' + response.status));
    }
    var payload = await response.json();
    embeddingState.defaults = payload.defaults || { embedding_model: '' };
    embeddingState.models = Array.isArray(payload.models) ? payload.models : [];
    embeddingState.messageSets = Array.isArray(payload.message_sets) ? payload.message_sets : [];
    embeddingState.runs = Array.isArray(payload.runs) ? payload.runs : [];

    if (embeddingState.selectedSetId) {
      var matching = embeddingState.messageSets.find(function(item) { return item.id === embeddingState.selectedSetId; });
      if (matching) {
        embeddingState.editorSet = embeddingClone(matching);
      }
    } else if (embeddingState.messageSets.length) {
      embeddingState.selectedSetId = embeddingState.messageSets[0].id;
      embeddingState.editorSet = embeddingClone(embeddingState.messageSets[0]);
    } else if (!embeddingState.editorSet) {
      embeddingState.editorSet = emptyMessageSet();
    }

    if (embeddingState.selectedRunId) {
      var selectedRun = embeddingState.runs.find(function(item) { return item.id === embeddingState.selectedRunId; });
      if (selectedRun) {
        embeddingState.selectedRun = embeddingClone(selectedRun);
      }
    }

    renderMessageSetList();
    renderMessageSetEditor();
    renderRunPanel();
    bindRunButtons();
    embeddingStatus('Ready.', false);
  } catch (error) {
    embeddingStatus(error.message || 'Failed to load embedding data.', true);
  }
}

function bindRunButtons() {
  document.querySelectorAll('[data-run-id]').forEach(function(node) {
    node.addEventListener('click', async function(event) {
      var runId = event.currentTarget.getAttribute('data-run-id');
      if (!runId) return;
      try {
        var response = await fetch('/embeddings/runs/' + encodeURIComponent(runId), { cache: 'no-store' });
        if (!response.ok) {
          throw new Error(await response.text() || ('request failed with status ' + response.status));
        }
        embeddingState.selectedRunId = runId;
        embeddingState.selectedRun = await response.json();
        renderRunPanel();
        bindRunButtons();
      } catch (error) {
        embeddingStatus(error.message || 'Failed to load run.', true);
      }
    });
  });
}

async function saveMessageSet() {
  var messageSet = embeddingClone(selectedMessageSet());
  try {
    var response = await fetch('/embeddings/message-sets', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(messageSet)
    });
    if (!response.ok) {
      throw new Error(await response.text() || ('request failed with status ' + response.status));
    }
    var saved = await response.json();
    embeddingState.selectedSetId = saved.id;
    embeddingState.editorSet = embeddingClone(saved);
    await refreshEmbeddingData();
    embeddingStatus('Message set saved.', false);
  } catch (error) {
    embeddingStatus(error.message || 'Failed to save message set.', true);
  }
}

async function deleteMessageSet() {
  var messageSet = selectedMessageSet();
  if (!messageSet.id) {
    embeddingState.editorSet = emptyMessageSet();
    renderMessageSetEditor();
    return;
  }
  try {
    var response = await fetch('/embeddings/message-sets/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: messageSet.id })
    });
    if (!response.ok) {
      throw new Error(await response.text() || ('request failed with status ' + response.status));
    }
    embeddingState.selectedSetId = '';
    embeddingState.editorSet = emptyMessageSet();
    embeddingState.selectedRun = null;
    embeddingState.selectedRunId = '';
    await refreshEmbeddingData();
    embeddingStatus('Message set deleted.', false);
  } catch (error) {
    embeddingStatus(error.message || 'Failed to delete message set.', true);
  }
}

async function runEmbeddingQuery() {
  var messageSet = selectedMessageSet();
  if (!messageSet.id) {
    embeddingStatus('Save the message set before running a query.', true);
    return;
  }

  var model = document.getElementById('embeddings-model');
  var query = document.getElementById('embeddings-query');
  var topk = document.getElementById('embeddings-topk');
  try {
    var response = await fetch('/embeddings/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        message_set_id: messageSet.id,
        embedding_model: model ? model.value : '',
        query: query ? query.value : '',
        top_k: topk ? Number(topk.value || 0) : 0
      })
    });
    if (!response.ok) {
      throw new Error(await response.text() || ('request failed with status ' + response.status));
    }
    embeddingState.selectedRun = await response.json();
    embeddingState.selectedRunId = embeddingState.selectedRun.id;
    await refreshEmbeddingData();
    embeddingStatus('Run completed.', false);
  } catch (error) {
    embeddingStatus(error.message || 'Failed to run query.', true);
  }
}

document.addEventListener('DOMContentLoaded', function() {
  document.getElementById('embeddings-reload-btn').addEventListener('click', refreshEmbeddingData);
  document.getElementById('embeddings-new-set-btn').addEventListener('click', function() {
    embeddingState.selectedSetId = '';
    embeddingState.editorSet = emptyMessageSet();
    renderMessageSetList();
    renderMessageSetEditor();
    renderRunPanel();
  });
  document.getElementById('embeddings-save-set-btn').addEventListener('click', saveMessageSet);
  document.getElementById('embeddings-delete-set-btn').addEventListener('click', deleteMessageSet);
  document.getElementById('embeddings-run-btn').addEventListener('click', runEmbeddingQuery);
  refreshEmbeddingData();
});
`
}
