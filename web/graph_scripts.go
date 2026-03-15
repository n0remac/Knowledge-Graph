package web

import . "github.com/n0remac/GoDom/html"

func GraphScripts() *Node {
	return Chl(
		Script(Src("https://unpkg.com/cytoscape/dist/cytoscape.min.js")),
		Script(Raw(GraphViewerBootstrapJS())),
	)
}

func GraphViewerBootstrapJS() string {
	return `
function graphStatus(message, isError) {
  var statusEl = document.getElementById('graph-status');
  if (!statusEl) {
    return;
  }
  statusEl.textContent = message;
  statusEl.className = isError ? 'text-sm text-red-600' : 'text-sm text-slate-500';
}

async function fetchGraphData() {
  var response = await fetch('/graph/data', { cache: 'no-store' });
  if (!response.ok) {
    throw new Error('failed to load graph data');
  }
  return await response.json();
}

function graphElements(payload) {
  if (payload && payload.elements) {
    return payload.elements;
  }
  return payload;
}

function selectedLayout() {
  var select = document.getElementById('graph-layout-select');
  return select ? select.value : 'breadthfirst';
}

function runLayout(cy, animate) {
  cy.layout({
    name: selectedLayout(),
    fit: true,
    padding: 48,
    animate: !!animate
  }).run();
}

function clearHighlights(cy) {
  cy.elements().removeClass('dimmed highlighted');
}

function highlightNeighborhood(cy, seedNodes) {
  var neighborhood = cy.collection();
  seedNodes.forEach(function(node) {
    neighborhood = neighborhood.union(node.closedNeighborhood());
  });

  cy.elements().addClass('dimmed').removeClass('highlighted');
  neighborhood.removeClass('dimmed').addClass('highlighted');
}

function applyMessageVisibility(cy) {
  var toggle = document.getElementById('graph-hide-messages-toggle');
  var hide = toggle ? toggle.checked : false;

  cy.nodes('[type = "message"]').forEach(function(node) {
    node.style('display', hide ? 'none' : 'element');
  });

  cy.edges().forEach(function(edge) {
    var sourceVisible = edge.source().style('display') !== 'none';
    var targetVisible = edge.target().style('display') !== 'none';
    edge.style('display', sourceVisible && targetVisible ? 'element' : 'none');
  });
}

function buildGraph(elements) {
  return cytoscape({
    container: document.getElementById('graph-canvas'),
    elements: elements,
    layout: {
      name: 'breadthfirst',
      fit: true,
      padding: 48,
      animate: false
    },
    wheelSensitivity: 0.2,
    style: [
      {
        selector: 'node',
        style: {
          'label': 'data(label)',
          'text-wrap': 'wrap',
          'text-max-width': 160,
          'font-size': 11,
          'font-family': 'ui-sans-serif, system-ui, sans-serif',
          'color': '#0f172a',
          'background-color': '#cbd5e1',
          'border-width': 1.5,
          'border-color': '#475569',
          'text-outline-color': '#f8fafc',
          'text-outline-width': 2,
          'padding': '8px',
          'transition-property': 'opacity, background-color, border-color',
          'transition-duration': '180ms'
        }
      },
      {
        selector: 'node[type = "topic"]',
        style: {
          'shape': 'roundrectangle',
          'background-color': '#67e8f9',
          'border-color': '#0f766e',
          'width': 148,
          'height': 54,
          'font-size': 13,
          'font-weight': '700'
        }
      },
      {
        selector: 'node[type = "fact"]',
        style: {
          'shape': 'roundrectangle',
          'background-color': '#fde68a',
          'border-color': '#b45309',
          'width': 172,
          'height': 64
        }
      },
      {
        selector: 'node[type = "fact"][status = "candidate"]',
        style: {
          'border-style': 'dashed'
        }
      },
      {
        selector: 'node[type = "user"]',
        style: {
          'shape': 'diamond',
          'background-color': '#bae6fd',
          'border-color': '#0369a1',
          'width': 68,
          'height': 68
        }
      },
      {
        selector: 'node[type = "message"]',
        style: {
          'shape': 'ellipse',
          'background-color': '#e2e8f0',
          'border-color': '#64748b',
          'width': 38,
          'height': 38,
          'font-size': 8,
          'opacity': 0.38
        }
      },
      {
        selector: 'edge',
        style: {
          'curve-style': 'bezier',
          'target-arrow-shape': 'triangle',
          'line-color': '#94a3b8',
          'target-arrow-color': '#94a3b8',
          'width': 1.4,
          'label': 'data(label)',
          'font-size': 8,
          'color': '#334155',
          'text-rotation': 'autorotate',
          'text-background-color': '#f8fafc',
          'text-background-opacity': 0.92,
          'text-background-padding': 2
        }
      },
      {
        selector: 'edge[type = "MENTIONS_TOPIC"]',
        style: {
          'line-style': 'dashed'
        }
      },
      {
        selector: 'edge[type = "FACT_FOR_USER"]',
        style: {
          'line-color': '#38bdf8',
          'target-arrow-color': '#38bdf8'
        }
      },
      {
        selector: '.dimmed',
        style: {
          'opacity': 0.1
        }
      },
      {
        selector: '.highlighted',
        style: {
          'opacity': 1
        }
      }
    ]
  });
}

async function reloadGraph(cy) {
  graphStatus('Reloading graph...', false);
  var payload = await fetchGraphData();
  var elements = graphElements(payload);
  cy.elements().remove();
  cy.add(elements);
  clearHighlights(cy);
  applyMessageVisibility(cy);
  runLayout(cy, false);
  graphStatus('Loaded ' + cy.nodes().length + ' nodes and ' + cy.edges().length + ' edges.', false);
}

function wireGraphControls(cy) {
  var reloadBtn = document.getElementById('graph-reload-btn');
  var layoutSelect = document.getElementById('graph-layout-select');
  var searchForm = document.getElementById('graph-search-form');
  var searchInput = document.getElementById('graph-search-input');
  var hideMessagesToggle = document.getElementById('graph-hide-messages-toggle');

  if (reloadBtn) {
    reloadBtn.addEventListener('click', async function() {
      try {
        await reloadGraph(cy);
      } catch (error) {
        console.error(error);
        graphStatus('Failed to reload graph.', true);
      }
    });
  }

  if (layoutSelect) {
    layoutSelect.addEventListener('change', function() {
      runLayout(cy, true);
    });
  }

  if (searchForm && searchInput) {
    searchForm.addEventListener('submit', function(event) {
      event.preventDefault();
      var query = searchInput.value.trim().toLowerCase();
      if (!query) {
        clearHighlights(cy);
        graphStatus('Cleared search highlight.', false);
        return;
      }

      var matches = cy.nodes().filter(function(node) {
        var haystack = String(node.data('searchText') || node.data('label') || '').toLowerCase();
        return haystack.indexOf(query) >= 0;
      });

      if (matches.length === 0) {
        clearHighlights(cy);
        graphStatus('No nodes matched "' + query + '".', true);
        return;
      }

      highlightNeighborhood(cy, matches);
      cy.fit(matches, 100);
      graphStatus('Matched ' + matches.length + ' node(s).', false);
    });
  }

  if (hideMessagesToggle) {
    hideMessagesToggle.addEventListener('change', function() {
      applyMessageVisibility(cy);
      graphStatus(hideMessagesToggle.checked ? 'Message nodes hidden.' : 'Message nodes visible.', false);
    });
  }

  cy.on('tap', 'node', function(event) {
    highlightNeighborhood(cy, cy.collection(event.target));
    graphStatus('Focused ' + String(event.target.data('label') || 'node') + '.', false);
  });

  cy.on('tap', function(event) {
    if (event.target === cy) {
      clearHighlights(cy);
      graphStatus('Selection cleared.', false);
    }
  });
}

async function bootKnowledgeGraph() {
  try {
    graphStatus('Loading graph...', false);
    var payload = await fetchGraphData();
    var elements = graphElements(payload);
    var cy = buildGraph(elements);
    wireGraphControls(cy);
    applyMessageVisibility(cy);
    graphStatus('Loaded ' + cy.nodes().length + ' nodes and ' + cy.edges().length + ' edges.', false);
    window.cyKnowledgeGraph = cy;
  } catch (error) {
    console.error(error);
    graphStatus('Failed to load graph.', true);
    var canvas = document.getElementById('graph-canvas');
    if (canvas) {
      canvas.innerHTML = '<div class="flex h-full items-center justify-center p-6 text-sm text-red-700">Failed to load graph.</div>';
    }
  }
}

document.addEventListener('DOMContentLoaded', bootKnowledgeGraph);
`
}
