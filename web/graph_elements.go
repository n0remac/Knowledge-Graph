package web

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/n0remac/Knowledge-Graph/internal/store"
)

type CytoscapeGraph struct {
	Elements CytoscapeElements `json:"elements"`
}

type CytoscapeElements struct {
	Nodes []CytoscapeNode `json:"nodes"`
	Edges []CytoscapeEdge `json:"edges"`
}

type CytoscapeNode struct {
	Data CytoscapeNodeData `json:"data"`
}

type CytoscapeNodeData struct {
	ID         string `json:"id"`
	RawID      string `json:"rawId"`
	Type       string `json:"type"`
	Label      string `json:"label"`
	SearchText string `json:"searchText,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Status     string `json:"status,omitempty"`
}

type CytoscapeEdge struct {
	Data CytoscapeEdgeData `json:"data"`
}

type CytoscapeEdgeData struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
	Label  string `json:"label"`
}

func BuildCytoscapeGraph(gs *store.Store) (CytoscapeGraph, error) {
	if gs == nil {
		return CytoscapeGraph{}, errors.New("graph store is nil")
	}

	snap := gs.SnapshotGraph()
	nodes := make([]CytoscapeNode, 0, len(snap.Users)+len(snap.Messages)+len(snap.Topics)+len(snap.Facts))
	edges := make([]CytoscapeEdge, 0, len(snap.Edges))
	nodeIDs := make(map[string]struct{}, cap(nodes))

	addNode := func(node CytoscapeNode) {
		if node.Data.ID == "" {
			return
		}
		if _, exists := nodeIDs[node.Data.ID]; exists {
			return
		}
		nodeIDs[node.Data.ID] = struct{}{}
		nodes = append(nodes, node)
	}

	for _, user := range snap.Users {
		label := firstNonEmpty(user.DisplayName, user.Username, user.ID)
		addNode(CytoscapeNode{
			Data: CytoscapeNodeData{
				ID:         nodeRef("user", user.ID),
				RawID:      user.ID,
				Type:       "user",
				Label:      label,
				SearchText: strings.ToLower(strings.TrimSpace(strings.Join([]string{label, user.Username, user.DisplayName, user.ID}, " "))),
			},
		})
	}

	for _, message := range snap.Messages {
		label := truncate(strings.TrimSpace(message.Content), 56)
		if label == "" {
			label = "Message " + shortID(message.ID)
		}
		addNode(CytoscapeNode{
			Data: CytoscapeNodeData{
				ID:         nodeRef("message", message.ID),
				RawID:      message.ID,
				Type:       "message",
				Label:      label,
				SearchText: strings.ToLower(strings.TrimSpace(strings.Join([]string{message.Author, message.AuthorID, message.Content, message.ChannelID, message.ID}, " "))),
			},
		})
	}

	for _, topic := range snap.Topics {
		rawID := int64String(topic.ID)
		addNode(CytoscapeNode{
			Data: CytoscapeNodeData{
				ID:         nodeRef("topic", rawID),
				RawID:      rawID,
				Type:       "topic",
				Label:      firstNonEmpty(topic.Name, rawID),
				SearchText: strings.ToLower(strings.TrimSpace(strings.Join([]string{topic.Name, topic.Kind, topic.Summary, rawID}, " "))),
				Kind:       topic.Kind,
			},
		})
	}

	for _, fact := range snap.Facts {
		rawID := int64String(fact.ID)
		label := truncate(strings.TrimSpace(fact.ValueText), 64)
		if label == "" {
			label = fmt.Sprintf("%s fact", firstNonEmpty(fact.Kind, "fact"))
		}
		addNode(CytoscapeNode{
			Data: CytoscapeNodeData{
				ID:         nodeRef("fact", rawID),
				RawID:      rawID,
				Type:       "fact",
				Label:      label,
				SearchText: strings.ToLower(strings.TrimSpace(strings.Join([]string{fact.Kind, fact.ValueText, fact.AboutType, fact.AboutID, fact.Status, rawID}, " "))),
				Kind:       fact.Kind,
				Status:     fact.Status,
			},
		})
	}

	for _, edge := range snap.Edges {
		source := nodeRef(edge.FromType, edge.FromID)
		target := nodeRef(edge.ToType, edge.ToID)
		if _, ok := nodeIDs[source]; !ok {
			continue
		}
		if _, ok := nodeIDs[target]; !ok {
			continue
		}

		edges = append(edges, CytoscapeEdge{
			Data: CytoscapeEdgeData{
				ID:     "edge:" + int64String(edge.ID),
				Source: source,
				Target: target,
				Type:   edge.EdgeType,
				Label:  edge.EdgeType,
			},
		})
	}

	return CytoscapeGraph{
		Elements: CytoscapeElements{
			Nodes: nodes,
			Edges: edges,
		},
	}, nil
}

func nodeRef(nodeType, rawID string) string {
	nodeType = strings.TrimSpace(nodeType)
	rawID = strings.TrimSpace(rawID)
	if nodeType == "" || rawID == "" {
		return ""
	}
	return nodeType + ":" + rawID
}

func int64String(v int64) string {
	return strconv.FormatInt(v, 10)
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 6 {
		return id
	}
	return id[len(id)-6:]
}

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	if n <= 3 {
		return string(r[:n])
	}
	return string(r[:n-3]) + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
