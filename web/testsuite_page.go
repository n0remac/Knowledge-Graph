package web

import . "github.com/n0remac/GoDom/html"

func TestSuitePage() *Node {
	return Html(
		Attr("data-theme", "dark"),
		pageHead("Transcript Test Harness", testSuitePageCSS()),
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
							H1(Class("text-2xl font-semibold tracking-tight text-[var(--app-fg)]"), T("Transcript Test Harness")),
							P(Class("text-sm text-[var(--app-fg-muted)]"), T("Create transcripts, run isolated tests, and inspect memory artifacts.")),
						),
						Button(
							Id("tests-reload-btn"),
							Type("button"),
							Class(uiPrimaryButtonClass("sm")),
							T("Reload"),
						),
						Div(
							Id("tests-status"),
							Class("text-sm text-[var(--app-fg-soft)]"),
							Attr("role", "status"),
							T("Loading transcripts..."),
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
								H2(Class("text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]"), T("Transcripts")),
								Button(Id("tests-new-transcript-btn"), Type("button"), Class(uiPrimaryButtonClass("xs")), T("New")),
							),
							Div(Id("tests-transcript-list"), Class("mt-4 flex max-h-[calc(100vh-12rem)] flex-col gap-2 overflow-y-auto pr-1")),
						),
						Section(
							Class("rounded-3xl border border-[var(--app-border-strong)] bg-[var(--app-surface)] p-4 shadow-sm backdrop-blur"),
							Div(
								Class("flex items-center justify-between gap-3"),
								H2(Class("text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]"), T("Transcript Editor")),
								Div(Class("flex items-center gap-2"),
									Button(Id("tests-delete-transcript-btn"), Type("button"), Class(uiDangerButtonClass("sm")), T("Delete")),
									Button(Id("tests-save-transcript-btn"), Type("button"), Class(uiPrimaryButtonClass("sm")), T("Save")),
								),
							),
							Div(Id("tests-transcript-editor"), Class("mt-4")),
						),
						Section(
							Class("rounded-3xl border border-[var(--app-border-strong)] bg-[var(--app-surface)] p-4 shadow-sm backdrop-blur"),
							Div(
								Class("flex items-center justify-between gap-3"),
								H2(Class("text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]"), T("Run Review")),
								Button(Id("tests-run-btn"), Type("button"), Class(uiPrimaryButtonClass("sm")), T("Run Transcript")),
							),
							Div(Id("tests-run-panel"), Class("mt-4 space-y-4")),
						),
					),
				),
			),
			Script(Raw(testSuitePageScript())),
		),
	)
}

func testSuitePageCSS() string {
	return `
.tests-card {
  transition: border-color 140ms ease, background-color 140ms ease, transform 140ms ease;
}

.tests-card.is-active {
  border-color: rgba(34, 211, 238, 0.68);
  background: rgba(8, 145, 178, 0.14);
  transform: translateY(-1px);
}

.tests-section {
  border: 1px solid var(--app-border);
  border-radius: 1.25rem;
  background: var(--app-surface-muted);
  padding: 1rem;
  box-shadow: 0 1px 0 rgba(255, 255, 255, 0.03);
}

.tests-pre {
  white-space: pre-wrap;
  word-break: break-word;
}

.tests-label {
  display: block;
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--app-fg-soft);
}
`
}

func testSuitePageScript() string {
	return `
var testSuiteState = {
  defaults: { chat_model: '', extract_model: '', persona: '' },
  models: [],
  configs: [],
  transcripts: [],
  runs: [],
  editorTranscript: null,
  runForm: null,
  selectedConfigId: '',
  selectedTranscriptId: '',
  selectedRunId: '',
  selectedRun: null,
  selectedRunConversation: null
};

var testsUi = {
  input: 'w-full rounded-2xl border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-3 py-2.5 text-sm text-[var(--app-fg)] shadow-sm outline-none transition placeholder:text-[var(--app-fg-soft)] focus:border-cyan-400/70 focus:ring-4 focus:ring-cyan-400/15',
  textarea: 'w-full min-h-24 resize-y rounded-2xl border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-3 py-2.5 text-sm text-[var(--app-fg)] shadow-sm outline-none transition placeholder:text-[var(--app-fg-soft)] focus:border-cyan-400/70 focus:ring-4 focus:ring-cyan-400/15',
  select: 'w-full rounded-2xl border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-3 py-2.5 pr-10 text-sm text-[var(--app-fg)] shadow-sm outline-none transition focus:border-cyan-400/70 focus:ring-4 focus:ring-cyan-400/15',
  primaryXs: 'inline-flex h-8 items-center justify-center rounded-xl border border-cyan-300/30 bg-cyan-400 px-3 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-slate-950 shadow-sm transition duration-150 hover:-translate-y-0.5 hover:bg-cyan-300 focus:outline-none focus:ring-4 focus:ring-cyan-400/25',
  primarySm: 'inline-flex h-9 items-center justify-center rounded-xl border border-cyan-300/30 bg-cyan-400 px-3.5 text-sm font-semibold text-slate-950 shadow-sm transition duration-150 hover:-translate-y-0.5 hover:bg-cyan-300 focus:outline-none focus:ring-4 focus:ring-cyan-400/25',
  secondarySm: 'inline-flex h-9 items-center justify-center rounded-xl border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-3.5 text-sm font-medium text-[var(--app-fg)] shadow-sm transition duration-150 hover:-translate-y-0.5 hover:border-cyan-400/50 hover:bg-cyan-500/10 focus:outline-none focus:ring-4 focus:ring-cyan-400/15',
  secondaryXs: 'inline-flex h-8 items-center justify-center rounded-xl border border-[var(--app-border-strong)] bg-[var(--app-panel)] px-3 text-[0.68rem] font-medium uppercase tracking-[0.18em] text-[var(--app-fg)] shadow-sm transition duration-150 hover:-translate-y-0.5 hover:border-cyan-400/50 hover:bg-cyan-500/10 focus:outline-none focus:ring-4 focus:ring-cyan-400/15',
  dangerSm: 'inline-flex h-9 items-center justify-center rounded-xl border border-red-500/35 bg-red-500/10 px-3.5 text-sm font-medium text-red-300 shadow-sm transition duration-150 hover:-translate-y-0.5 hover:bg-red-500/16 focus:outline-none focus:ring-4 focus:ring-red-500/15',
  dangerXs: 'inline-flex h-8 items-center justify-center rounded-xl border border-red-500/35 bg-red-500/10 px-3 text-[0.68rem] font-medium uppercase tracking-[0.18em] text-red-300 shadow-sm transition duration-150 hover:-translate-y-0.5 hover:bg-red-500/16 focus:outline-none focus:ring-4 focus:ring-red-500/15'
};

function testsStatus(message, isError) {
  var el = document.getElementById('tests-status');
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
  return '<section class="tests-section">' +
    '<h3 class="text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">' + escapeHtml(title) + '</h3>' +
    '<div class="mt-3">' + body + '</div>' +
    '</section>';
}

function clone(value) {
  return JSON.parse(JSON.stringify(value));
}

function selectOptions(models, selectedValue) {
  var list = Array.isArray(models) ? models.slice() : [];
  var selected = String(selectedValue || '').trim();
  if (selected && !list.some(function(item) { return (item && item.name) === selected; })) {
    list = [{ name: selected }].concat(list);
  }
  return list.map(function(item) {
    var name = item && item.name ? String(item.name) : '';
    if (!name) return '';
    var selectedAttr = name === selected ? ' selected' : '';
    return '<option value="' + escapeHtml(name) + '"' + selectedAttr + '>' + escapeHtml(name) + '</option>';
  }).join('');
}

function emptyTranscript() {
  return {
    id: '',
    name: '',
    description: '',
    steps: [{ message: '' }]
  };
}

function defaultRunForm() {
  return {
    id: '',
    name: '',
    chat_model: (testSuiteState.defaults && testSuiteState.defaults.chat_model) || '',
    extract_model: (testSuiteState.defaults && testSuiteState.defaults.extract_model) || '',
    persona: (testSuiteState.defaults && testSuiteState.defaults.persona) || ''
  };
}

function selectedTranscript() {
  return testSuiteState.editorTranscript || emptyTranscript();
}

function selectedRunForm() {
  return testSuiteState.runForm || defaultRunForm();
}

function selectedConversation() {
  if (!testSuiteState.selectedRunConversation || !testSuiteState.selectedRunConversation.conversations || !testSuiteState.selectedRunConversation.conversations.length) {
    return null;
  }
  return testSuiteState.selectedRunConversation.conversations[0];
}

function formatTimestamp(unixMs) {
  if (!unixMs) return 'n/a';
  try {
    return new Date(unixMs).toLocaleString();
  } catch (err) {
    return String(unixMs);
  }
}

function renderTranscriptList() {
  var list = document.getElementById('tests-transcript-list');
  if (!list) return;
  list.innerHTML = '';

  if (!testSuiteState.transcripts.length) {
    list.innerHTML = '<div class="rounded-2xl border border-dashed border-[var(--app-border)] px-4 py-8 text-center text-sm text-[var(--app-fg-soft)]">No saved transcripts yet.</div>';
    return;
  }

  testSuiteState.transcripts.forEach(function(transcript) {
    var button = document.createElement('button');
    button.type = 'button';
    button.className = 'tests-card rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3 text-left text-[var(--app-fg)] shadow-sm transition hover:-translate-y-0.5 hover:border-cyan-400/60 hover:bg-cyan-500/10';
    if (transcript.id === testSuiteState.selectedTranscriptId) {
      button.classList.add('is-active');
    }
    button.innerHTML =
      '<div class="flex items-center justify-between gap-3">' +
        '<span class="truncate font-semibold text-[var(--app-fg)]">' + escapeHtml(transcript.name) + '</span>' +
        '<span class="rounded-full border border-cyan-400/30 bg-cyan-400/12 px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-cyan-200">' + escapeHtml(transcript.steps.length) + ' steps</span>' +
      '</div>' +
      '<p class="mt-2 text-xs text-[var(--app-fg-muted)]">' + escapeHtml(transcript.description || 'No description.') + '</p>';
    button.addEventListener('click', function() {
      testSuiteState.selectedTranscriptId = transcript.id;
      testSuiteState.editorTranscript = clone(transcript);
      renderTranscriptList();
      renderTranscriptEditor();
    });
    list.appendChild(button);
  });
}

function renderTranscriptEditor() {
  var editor = document.getElementById('tests-transcript-editor');
  if (!editor) return;
  var transcript = selectedTranscript();
  var steps = transcript.steps || [];

  editor.innerHTML =
    '<div class="space-y-4">' +
      '<div>' +
        '<label class="tests-label" for="tests-transcript-name">Name</label>' +
        '<input id="tests-transcript-name" class="mt-2 ' + testsUi.input + '" value="' + escapeHtml(transcript.name || '') + '" />' +
      '</div>' +
      '<div>' +
        '<label class="tests-label" for="tests-transcript-description">Description</label>' +
        '<textarea id="tests-transcript-description" class="mt-2 ' + testsUi.textarea + '">' + escapeHtml(transcript.description || '') + '</textarea>' +
      '</div>' +
      '<div class="flex items-center justify-between gap-3">' +
        '<span class="tests-label">Messages</span>' +
        '<button id="tests-add-step-btn" type="button" class="' + testsUi.primaryXs + '">Add Message</button>' +
      '</div>' +
      '<div id="tests-steps-list" class="space-y-3"></div>' +
    '</div>';

  var stepsList = document.getElementById('tests-steps-list');
  steps.forEach(function(step, index) {
    var stepNode = document.createElement('div');
    stepNode.className = 'rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-3';
    stepNode.innerHTML =
      '<div class="flex items-center justify-between gap-3">' +
        '<span class="tests-label">Step ' + escapeHtml(index + 1) + '</span>' +
        '<div class="flex items-center gap-2">' +
          '<button data-action="up" data-index="' + escapeHtml(index) + '" type="button" class="' + testsUi.secondaryXs + '">Up</button>' +
          '<button data-action="down" data-index="' + escapeHtml(index) + '" type="button" class="' + testsUi.secondaryXs + '">Down</button>' +
          '<button data-action="delete" data-index="' + escapeHtml(index) + '" type="button" class="' + testsUi.dangerXs + '">Delete</button>' +
        '</div>' +
      '</div>' +
      '<textarea data-step-message="' + escapeHtml(index) + '" class="mt-2 min-h-28 ' + testsUi.textarea + '">' + escapeHtml(step.message || '') + '</textarea>';
    stepsList.appendChild(stepNode);
  });

  editor.querySelectorAll('[data-step-message]').forEach(function(node) {
    node.addEventListener('input', function(event) {
      var index = Number(event.target.getAttribute('data-step-message'));
      testSuiteState.editorTranscript.steps[index].message = event.target.value;
    });
  });
  editor.querySelectorAll('[data-action]').forEach(function(node) {
    node.addEventListener('click', function(event) {
      var index = Number(event.target.getAttribute('data-index'));
      var action = event.target.getAttribute('data-action');
      if (action === 'delete') {
        testSuiteState.editorTranscript.steps.splice(index, 1);
        if (!testSuiteState.editorTranscript.steps.length) {
          testSuiteState.editorTranscript.steps.push({ message: '' });
        }
      } else if (action === 'up' && index > 0) {
        var current = testSuiteState.editorTranscript.steps[index];
        testSuiteState.editorTranscript.steps[index] = testSuiteState.editorTranscript.steps[index - 1];
        testSuiteState.editorTranscript.steps[index - 1] = current;
      } else if (action === 'down' && index < testSuiteState.editorTranscript.steps.length - 1) {
        var next = testSuiteState.editorTranscript.steps[index];
        testSuiteState.editorTranscript.steps[index] = testSuiteState.editorTranscript.steps[index + 1];
        testSuiteState.editorTranscript.steps[index + 1] = next;
      }
      renderTranscriptEditor();
    });
  });

  document.getElementById('tests-transcript-name').addEventListener('input', function(event) {
    testSuiteState.editorTranscript.name = event.target.value;
  });
  document.getElementById('tests-transcript-description').addEventListener('input', function(event) {
    testSuiteState.editorTranscript.description = event.target.value;
  });
  document.getElementById('tests-add-step-btn').addEventListener('click', function() {
    testSuiteState.editorTranscript.steps.push({ message: '' });
    renderTranscriptEditor();
  });
}

function renderRunPanel() {
  var panel = document.getElementById('tests-run-panel');
  if (!panel) return;
  var defaults = testSuiteState.defaults || {};
  var models = testSuiteState.models || [];
  var configs = testSuiteState.configs || [];
  var runForm = selectedRunForm();
  var run = testSuiteState.selectedRun;
  var conversation = selectedConversation();

  var runsList = (testSuiteState.runs || []).map(function(item) {
    return '<button type="button" data-run-id="' + escapeHtml(item.id) + '" class="tests-card w-full rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3 text-left text-[var(--app-fg)] shadow-sm transition hover:-translate-y-0.5 hover:border-cyan-400/60 hover:bg-cyan-500/10 ' + (item.id === testSuiteState.selectedRunId ? 'is-active' : '') + '">' +
      '<div class="flex items-center justify-between gap-3">' +
        '<span class="truncate font-semibold text-[var(--app-fg)]">' + escapeHtml(item.transcript_name) + '</span>' +
        '<span class="rounded-full px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] ' + (item.status === 'completed' ? 'border border-emerald-400/30 bg-emerald-400/12 text-emerald-200' : item.status === 'failed' ? 'border border-red-500/35 bg-red-500/12 text-red-200' : 'border border-[var(--app-border-strong)] bg-[var(--app-panel)] text-[var(--app-fg-muted)]') + '">' + escapeHtml(item.status) + '</span>' +
      '</div>' +
      '<p class="mt-2 text-xs text-[var(--app-fg-muted)]">' + escapeHtml(item.chat_model) + ' / ' + escapeHtml(item.extract_model) + '</p>' +
      '<p class="mt-1 text-[0.72rem] text-[var(--app-fg-soft)]">' + escapeHtml(formatTimestamp(item.started_at_unix_ms)) + '</p>' +
    '</button>';
  }).join('') || '<div class="text-sm text-[var(--app-fg-soft)]">No runs yet.</div>';

  var steps = '';
  if (run && run.steps && run.steps.length) {
    steps = run.steps.map(function(step) {
      return '<section class="tests-section">' +
        '<div class="flex items-center justify-between gap-3">' +
          '<h3 class="text-sm font-semibold uppercase tracking-[0.18em] text-[var(--app-fg-soft)]">Step ' + escapeHtml(step.index) + '</h3>' +
          '<span class="text-xs text-[var(--app-fg-soft)]">state v' + escapeHtml(step.working_state_version) + '</span>' +
        '</div>' +
        '<div class="mt-3 space-y-3">' +
          '<div><p class="tests-label">User Message</p><pre class="tests-pre mt-2 rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)] p-3 text-sm text-[var(--app-fg)]">' + escapeHtml(step.user_message || '') + '</pre></div>' +
          '<div><p class="tests-label">Assistant Reply</p><pre class="tests-pre mt-2 rounded-xl border border-slate-800 bg-slate-950/90 p-3 text-sm text-slate-100">' + escapeHtml(step.assistant_reply || '') + '</pre></div>' +
          '<div><p class="tests-label">Rolling Summary</p><pre class="tests-pre mt-2 rounded-xl border border-[var(--app-border)] bg-[var(--app-panel)] p-3 text-sm text-[var(--app-fg)]">' + escapeHtml(step.rolling_summary || '') + '</pre></div>' +
          '<div><p class="tests-label">Response Brief</p><pre class="tests-pre mt-2 rounded-xl border border-slate-800 bg-slate-950/90 p-3 text-xs text-slate-100">' + escapeHtml(step.response_brief || '') + '</pre></div>' +
          '<div class="text-xs text-[var(--app-fg-soft)]">Summary update: ' + escapeHtml(step.summary_update_status || 'n/a') + ' | Trace IDs: ' + escapeHtml((step.trace_ids || []).join(', ')) + '</div>' +
        '</div>' +
      '</section>';
    }).join('');
  } else if (run) {
    steps = '<div class="text-sm text-[var(--app-fg-soft)]">This run has no completed steps.</div>';
  } else {
    steps = '<div class="text-sm text-[var(--app-fg-soft)]">Select a run to inspect its results.</div>';
  }

  var conversationHtml = '<div class="text-sm text-[var(--app-fg-soft)]">Load isolated conversation artifacts for the selected run.</div>';
  if (conversation) {
    conversationHtml =
      '<div class="space-y-3">' +
        '<div class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] p-4">' +
          '<div class="flex items-center justify-between gap-3">' +
            '<span class="font-semibold text-[var(--app-fg)]">' + escapeHtml(conversation.conversation_id) + '</span>' +
            '<span class="rounded-full border border-cyan-400/30 bg-cyan-400/12 px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-cyan-200">' + escapeHtml(conversation.message_count) + ' messages</span>' +
          '</div>' +
          '<p class="mt-2 text-sm text-[var(--app-fg-muted)]">Latest message: ' + escapeHtml((conversation.latest_message && conversation.latest_message.content) || '') + '</p>' +
        '</div>' +
        '<div class="grid gap-3 lg:grid-cols-2">' +
          '<pre class="tests-pre rounded-xl border border-slate-800 bg-slate-950/90 p-4 text-xs text-slate-100">' + prettyJson(conversation.working_state || {}) + '</pre>' +
          '<pre class="tests-pre rounded-xl border border-[var(--app-border)] bg-[var(--app-surface)] p-4 text-xs text-[var(--app-fg)]">' + prettyJson(conversation.latest_response_context || {}) + '</pre>' +
        '</div>' +
        '<pre class="tests-pre rounded-xl border border-slate-800 bg-slate-950/90 p-4 text-xs text-slate-100">' + prettyJson(conversation.latest_trace || {}) + '</pre>' +
      '</div>';
  }

  panel.innerHTML =
    section('Run Settings',
      '<div class="grid gap-4">' +
        '<div>' +
          '<label class="tests-label" for="tests-run-config-select">Saved Configuration</label>' +
          '<div class="mt-2 flex gap-2">' +
            '<select id="tests-run-config-select" class="' + testsUi.select + '">' +
              '<option value="">Custom / Defaults</option>' +
              configs.map(function(config) {
                var selectedAttr = config.id === testSuiteState.selectedConfigId ? ' selected' : '';
                return '<option value="' + escapeHtml(config.id) + '"' + selectedAttr + '>' + escapeHtml(config.name) + '</option>';
              }).join('') +
            '</select>' +
            '<button id="tests-new-config-btn" type="button" class="' + testsUi.secondarySm + '">New</button>' +
          '</div>' +
        '</div>' +
        '<div>' +
          '<label class="tests-label" for="tests-run-config-name">Configuration Name</label>' +
          '<div class="mt-2 flex gap-2">' +
            '<input id="tests-run-config-name" class="' + testsUi.input + '" value="' + escapeHtml(runForm.name || '') + '" />' +
            '<button id="tests-save-config-btn" type="button" class="' + testsUi.primarySm + '">Save Config</button>' +
            '<button id="tests-delete-config-btn" type="button" class="' + testsUi.dangerSm + '">Delete</button>' +
          '</div>' +
        '</div>' +
        '<div><label class="tests-label" for="tests-chat-model">Chat Model</label><select id="tests-chat-model" class="mt-2 ' + testsUi.select + '">' + selectOptions(models, runForm.chat_model || defaults.chat_model || '') + '</select></div>' +
        '<div><label class="tests-label" for="tests-extract-model">Extract Model</label><select id="tests-extract-model" class="mt-2 ' + testsUi.select + '">' + selectOptions(models, runForm.extract_model || defaults.extract_model || '') + '</select></div>' +
        '<div><label class="tests-label" for="tests-persona">Persona</label><textarea id="tests-persona" class="mt-2 ' + testsUi.textarea + '">' + escapeHtml(runForm.persona || defaults.persona || '') + '</textarea></div>' +
      '</div>') +
    section('Recent Runs', '<div id="tests-runs-list" class="space-y-2">' + runsList + '</div>') +
    section('Selected Run',
      run ? (
        '<div class="space-y-3">' +
          '<div class="rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] p-4">' +
            '<div class="flex items-center justify-between gap-3">' +
              '<div><p class="font-semibold text-[var(--app-fg)]">' + escapeHtml(run.transcript_name) + '</p><p class="text-xs text-[var(--app-fg-soft)]">' + escapeHtml(run.id) + '</p></div>' +
              '<span class="rounded-full px-2 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.18em] ' + (run.status === 'completed' ? 'border border-emerald-400/30 bg-emerald-400/12 text-emerald-200' : run.status === 'failed' ? 'border border-red-500/35 bg-red-500/12 text-red-200' : 'border border-[var(--app-border-strong)] bg-[var(--app-surface-muted)] text-[var(--app-fg-muted)]') + '">' + escapeHtml(run.status) + '</span>' +
            '</div>' +
            '<p class="mt-2 text-xs text-[var(--app-fg-soft)]">Started: ' + escapeHtml(formatTimestamp(run.started_at_unix_ms)) + '</p>' +
            '<p class="mt-1 text-xs text-[var(--app-fg-soft)]">Chat: ' + escapeHtml(run.chat_model) + ' | Extract: ' + escapeHtml(run.extract_model) + '</p>' +
            '<p class="mt-1 text-xs text-[var(--app-fg-soft)]">Store: ' + escapeHtml(run.store_path) + '</p>' +
            '<p class="mt-1 text-xs text-[var(--app-fg-soft)]">Telemetry: ' + escapeHtml(run.telemetry_dir) + '</p>' +
            (run.error ? '<pre class="tests-pre mt-3 rounded-xl border border-red-500/35 bg-red-500/10 p-3 text-xs text-red-200">' + escapeHtml(run.error) + '</pre>' : '') +
            '<div class="mt-3"><button id="tests-load-run-conversation-btn" type="button" class="' + testsUi.secondarySm + '">Load Conversation Artifacts</button></div>' +
          '</div>' +
          steps +
        '</div>'
      ) : '<div class="text-sm text-[var(--app-fg-soft)]">Select a run to review.</div>') +
    section('Isolated Conversation View', conversationHtml);

  document.querySelectorAll('[data-run-id]').forEach(function(node) {
    node.addEventListener('click', function(event) {
      var runID = event.currentTarget.getAttribute('data-run-id');
      loadRun(runID);
    });
  });
  var loadConversationBtn = document.getElementById('tests-load-run-conversation-btn');
  if (loadConversationBtn) {
    loadConversationBtn.addEventListener('click', function() {
      loadRunConversation();
    });
  }
  document.getElementById('tests-run-config-select').addEventListener('change', function(event) {
    var configID = event.target.value;
    if (!configID) {
      testSuiteState.selectedConfigId = '';
      testSuiteState.runForm = defaultRunForm();
    } else {
      var config = testSuiteState.configs.find(function(item) { return item.id === configID; });
      if (config) {
        testSuiteState.selectedConfigId = config.id;
        testSuiteState.runForm = clone(config);
      }
    }
    renderRunPanel();
  });
  document.getElementById('tests-new-config-btn').addEventListener('click', function() {
    testSuiteState.selectedConfigId = '';
    testSuiteState.runForm = defaultRunForm();
    renderRunPanel();
  });
  document.getElementById('tests-run-config-name').addEventListener('input', function(event) {
    testSuiteState.runForm.name = event.target.value;
  });
  document.getElementById('tests-chat-model').addEventListener('change', function(event) {
    testSuiteState.runForm.chat_model = event.target.value;
  });
  document.getElementById('tests-extract-model').addEventListener('change', function(event) {
    testSuiteState.runForm.extract_model = event.target.value;
  });
  document.getElementById('tests-persona').addEventListener('input', function(event) {
    testSuiteState.runForm.persona = event.target.value;
  });
  document.getElementById('tests-save-config-btn').addEventListener('click', function() {
    saveRunConfig();
  });
  document.getElementById('tests-delete-config-btn').addEventListener('click', function() {
    deleteRunConfig();
  });
}

async function loadData() {
  testsStatus('Loading test suite data...', false);
  try {
    var response = await fetch('/tests/data', { cache: 'no-store' });
    if (!response.ok) {
      throw new Error('Request failed with status ' + response.status);
    }
    var payload = await response.json();
    testSuiteState.defaults = payload.defaults || { chat_model: '', extract_model: '', persona: '' };
    testSuiteState.models = payload.models || [];
    testSuiteState.configs = payload.configs || [];
    testSuiteState.transcripts = payload.transcripts || [];
    testSuiteState.runs = payload.runs || [];
    if (!testSuiteState.runForm) {
      testSuiteState.runForm = defaultRunForm();
    }
    if (testSuiteState.selectedConfigId) {
      var selectedConfig = testSuiteState.configs.find(function(item) { return item.id === testSuiteState.selectedConfigId; });
      if (selectedConfig) {
        testSuiteState.runForm = clone(selectedConfig);
      } else {
        testSuiteState.selectedConfigId = '';
      }
    }

    if (testSuiteState.selectedTranscriptId) {
      var selected = testSuiteState.transcripts.find(function(item) { return item.id === testSuiteState.selectedTranscriptId; });
      if (selected) {
        testSuiteState.editorTranscript = clone(selected);
      }
    }
    if (!testSuiteState.editorTranscript) {
      if (testSuiteState.transcripts.length) {
        testSuiteState.selectedTranscriptId = testSuiteState.transcripts[0].id;
        testSuiteState.editorTranscript = clone(testSuiteState.transcripts[0]);
      } else {
        testSuiteState.selectedTranscriptId = '';
        testSuiteState.editorTranscript = emptyTranscript();
      }
    }

    if (!testSuiteState.selectedRun && testSuiteState.runs.length) {
      testSuiteState.selectedRunId = testSuiteState.runs[0].id;
      testSuiteState.selectedRun = testSuiteState.runs[0];
    }

    renderTranscriptList();
    renderTranscriptEditor();
    renderRunPanel();
    testsStatus('Test suite ready.', false);
  } catch (err) {
    testsStatus(err.message || 'Failed to load test suite data.', true);
  }
}

async function saveTranscript() {
  var transcript = clone(selectedTranscript());
  try {
    var response = await fetch('/tests/transcripts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(transcript)
    });
    if (!response.ok) {
      throw new Error(await response.text());
    }
    var saved = await response.json();
    testSuiteState.selectedTranscriptId = saved.id;
    testSuiteState.editorTranscript = clone(saved);
    await loadData();
    testsStatus('Transcript saved.', false);
  } catch (err) {
    testsStatus((err.message || 'Failed to save transcript.').trim(), true);
  }
}

async function deleteTranscript() {
  var transcript = selectedTranscript();
  if (!transcript.id) {
    testSuiteState.editorTranscript = emptyTranscript();
    renderTranscriptEditor();
    testsStatus('Unsaved transcript cleared.', false);
    return;
  }
  if (!window.confirm('Delete transcript "' + transcript.name + '"?')) {
    return;
  }
  try {
    var response = await fetch('/tests/transcripts/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: transcript.id })
    });
    if (!response.ok) {
      throw new Error(await response.text());
    }
    testSuiteState.selectedTranscriptId = '';
    testSuiteState.editorTranscript = null;
    await loadData();
    testsStatus('Transcript deleted.', false);
  } catch (err) {
    testsStatus((err.message || 'Failed to delete transcript.').trim(), true);
  }
}

async function runTranscript() {
  var transcript = selectedTranscript();
  var runForm = selectedRunForm();
  if (!transcript.id) {
    testsStatus('Save the transcript before running it.', true);
    return;
  }
  try {
    testsStatus('Running transcript...', false);
    var response = await fetch('/tests/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        transcript_id: transcript.id,
        chat_model: runForm.chat_model,
        extract_model: runForm.extract_model,
        persona: runForm.persona
      })
    });
    if (!response.ok) {
      throw new Error(await response.text());
    }
    var run = await response.json();
    testSuiteState.selectedRunId = run.id;
    testSuiteState.selectedRun = run;
    testSuiteState.selectedRunConversation = null;
    await loadData();
    await loadRun(run.id);
    testsStatus('Transcript run complete.', run.status !== 'completed');
  } catch (err) {
    testsStatus((err.message || 'Failed to run transcript.').trim(), true);
  }
}

async function saveRunConfig() {
  var runForm = selectedRunForm();
  try {
    var response = await fetch('/tests/configs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        id: testSuiteState.selectedConfigId,
        name: runForm.name,
        chat_model: runForm.chat_model,
        extract_model: runForm.extract_model,
        persona: runForm.persona
      })
    });
    if (!response.ok) {
      throw new Error(await response.text());
    }
    var saved = await response.json();
    testSuiteState.selectedConfigId = saved.id;
    testSuiteState.runForm = clone(saved);
    await loadData();
    testsStatus('Run configuration saved.', false);
  } catch (err) {
    testsStatus((err.message || 'Failed to save run configuration.').trim(), true);
  }
}

async function deleteRunConfig() {
  if (!testSuiteState.selectedConfigId) {
    testSuiteState.runForm = defaultRunForm();
    renderRunPanel();
    testsStatus('Unsaved run configuration cleared.', false);
    return;
  }
  if (!window.confirm('Delete this saved run configuration?')) {
    return;
  }
  try {
    var response = await fetch('/tests/configs/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: testSuiteState.selectedConfigId })
    });
    if (!response.ok) {
      throw new Error(await response.text());
    }
    testSuiteState.selectedConfigId = '';
    testSuiteState.runForm = defaultRunForm();
    await loadData();
    testsStatus('Run configuration deleted.', false);
  } catch (err) {
    testsStatus((err.message || 'Failed to delete run configuration.').trim(), true);
  }
}

async function loadRun(runID) {
  if (!runID) return;
  try {
    var response = await fetch('/tests/runs/' + encodeURIComponent(runID), { cache: 'no-store' });
    if (!response.ok) {
      throw new Error(await response.text());
    }
    testSuiteState.selectedRunId = runID;
    testSuiteState.selectedRun = await response.json();
    testSuiteState.selectedRunConversation = null;
    renderRunPanel();
  } catch (err) {
    testsStatus((err.message || 'Failed to load run.').trim(), true);
  }
}

async function loadRunConversation() {
  if (!testSuiteState.selectedRunId) return;
  try {
    var response = await fetch('/tests/runs/' + encodeURIComponent(testSuiteState.selectedRunId) + '/conversation', { cache: 'no-store' });
    if (!response.ok) {
      throw new Error(await response.text());
    }
    testSuiteState.selectedRunConversation = await response.json();
    renderRunPanel();
  } catch (err) {
    testsStatus((err.message || 'Failed to load run conversation.').trim(), true);
  }
}

document.getElementById('tests-reload-btn').addEventListener('click', function() {
  loadData();
});
document.getElementById('tests-new-transcript-btn').addEventListener('click', function() {
  testSuiteState.selectedTranscriptId = '';
  testSuiteState.editorTranscript = emptyTranscript();
  renderTranscriptList();
  renderTranscriptEditor();
});
document.getElementById('tests-save-transcript-btn').addEventListener('click', function() {
  saveTranscript();
});
document.getElementById('tests-delete-transcript-btn').addEventListener('click', function() {
  deleteTranscript();
});
document.getElementById('tests-run-btn').addEventListener('click', function() {
  runTranscript();
});

loadData();
`
}
