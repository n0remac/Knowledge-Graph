package store

import (
	"sort"

	"github.com/n0remac/Knowledge-Graph/internal/models"
)

type GraphUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

type GraphSnapshot struct {
	Users    []GraphUser      `json:"users"`
	Messages []models.Message `json:"messages"`
	Topics   []models.Topic   `json:"topics"`
	Facts    []models.Fact    `json:"facts"`
	Edges    []models.Edge    `json:"edges"`
}

func (s *Store) SnapshotGraph() GraphSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := GraphSnapshot{
		Users:    make([]GraphUser, 0, len(s.data.Users)),
		Messages: make([]models.Message, 0, len(s.data.Messages)),
		Topics:   make([]models.Topic, 0, len(s.data.Topics)),
		Facts:    make([]models.Fact, 0, len(s.data.Facts)),
		Edges:    make([]models.Edge, 0, len(s.data.Edges)),
	}

	for _, user := range s.data.Users {
		snapshot.Users = append(snapshot.Users, GraphUser{
			ID:          user.ID,
			Username:    user.Username,
			DisplayName: user.DisplayName,
		})
	}
	for _, message := range s.data.Messages {
		snapshot.Messages = append(snapshot.Messages, message)
	}
	for _, topic := range s.data.Topics {
		snapshot.Topics = append(snapshot.Topics, topic)
	}
	for _, fact := range s.data.Facts {
		snapshot.Facts = append(snapshot.Facts, fact)
	}
	for _, edge := range s.data.Edges {
		snapshot.Edges = append(snapshot.Edges, edge)
	}

	sort.Slice(snapshot.Users, func(i, j int) bool {
		return snapshot.Users[i].ID < snapshot.Users[j].ID
	})
	sort.Slice(snapshot.Messages, func(i, j int) bool {
		if snapshot.Messages[i].Timestamp.Equal(snapshot.Messages[j].Timestamp) {
			return snapshot.Messages[i].ID < snapshot.Messages[j].ID
		}
		return snapshot.Messages[i].Timestamp.Before(snapshot.Messages[j].Timestamp)
	})
	sort.Slice(snapshot.Topics, func(i, j int) bool {
		return snapshot.Topics[i].ID < snapshot.Topics[j].ID
	})
	sort.Slice(snapshot.Facts, func(i, j int) bool {
		return snapshot.Facts[i].ID < snapshot.Facts[j].ID
	})
	sort.Slice(snapshot.Edges, func(i, j int) bool {
		return snapshot.Edges[i].ID < snapshot.Edges[j].ID
	})

	return snapshot
}
