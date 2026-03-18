package conversation

import (
	"sort"
	"strings"
	"unicode"

	"github.com/n0remac/Knowledge-Graph/internal/models"
)

const (
	maxPromptRecentMessages   = 5
	maxRecentStateEntries     = 10
	maxResponseRecentMessages = 4
	maxActiveTopics           = 6
	maxActiveClaims           = 8
	maxBriefClaims            = 6
	newTopicSalience          = 0.75
	topicSeenBump             = 0.20
	topicDecay                = 0.10
	topicActiveThreshold      = 0.45
	topicPruneThreshold       = 0.20
	newClaimSalience          = 0.70
	claimSeenBump             = 0.15
	claimDecay                = 0.07
	claimPruneThreshold       = 0.25
)

func mergeWorkingState(previous models.WorkingState, current models.RawMessage, extraction models.MessageExtraction, rollingSummary string, _ func(string) int64) models.WorkingState {
	state := previous
	state.ConversationID = current.ConversationID
	state.LastUpdatedMessageID = current.MessageID
	state.StateVersion = previous.StateVersion + 1
	if strings.TrimSpace(rollingSummary) != "" {
		state.RollingSummary = strings.TrimSpace(rollingSummary)
	}

	state.ActiveTopics = mergeTopics(previous.ActiveTopics, extraction.ActiveTopics, current.MessageID)
	state.ActiveClaims = mergeClaims(previous.ActiveClaims, extraction.Claims, current.MessageID)
	state.RecentMessageIDs = appendBoundedUnique(previous.RecentMessageIDs, current.MessageID, maxRecentStateEntries)
	state.RecentExtractionIDs = appendBoundedUnique(previous.RecentExtractionIDs, extraction.MessageID, maxRecentStateEntries)
	return state
}

func mergeTopics(previous []models.TopicState, current []models.TopicRef, messageID string) []models.TopicState {
	currentSeen := make(map[string]struct{}, len(current))
	for _, topic := range current {
		name := normalizeTopicName(topic.Name)
		if name == "" {
			continue
		}
		currentSeen[name] = struct{}{}
	}

	merged := make(map[string]models.TopicState, len(previous)+len(currentSeen))
	for _, topic := range previous {
		name := normalizeTopicName(topic.Name)
		if name == "" {
			continue
		}
		topic.Name = name
		if _, seen := currentSeen[name]; seen {
			topic.Salience = clamp(topic.Salience+topicSeenBump, 0, 1)
			topic.LastSeenIn = messageID
		} else {
			topic.Salience = clamp(topic.Salience-topicDecay, 0, 1)
		}
		topic.Status = topicStatus(topic.Salience)
		if topic.Salience < topicPruneThreshold {
			continue
		}
		merged[name] = topic
	}

	for name := range currentSeen {
		if existing, ok := merged[name]; ok {
			existing.Salience = clamp(existing.Salience+topicSeenBump, 0, 1)
			existing.LastSeenIn = messageID
			existing.Status = topicStatus(existing.Salience)
			merged[name] = existing
			continue
		}
		merged[name] = models.TopicState{
			Name:       name,
			Salience:   newTopicSalience,
			Status:     topicStatus(newTopicSalience),
			LastSeenIn: messageID,
		}
	}

	out := make([]models.TopicState, 0, len(merged))
	for _, topic := range merged {
		out = append(out, topic)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Salience == out[j].Salience {
			return out[i].Name < out[j].Name
		}
		return out[i].Salience > out[j].Salience
	})
	if len(out) > maxActiveTopics {
		out = out[:maxActiveTopics]
	}
	return out
}

func mergeClaims(previous []models.ClaimState, current []models.Claim, messageID string) []models.ClaimState {
	currentSeen := make(map[string]models.Claim, len(current))
	for _, claim := range current {
		key := claimKey(claim.Subject, claim.Predicate, claim.Object)
		if key == "" {
			continue
		}
		currentSeen[key] = claim
	}

	merged := make(map[string]models.ClaimState, len(previous)+len(currentSeen))
	for _, claim := range previous {
		key := claimKey(claim.Subject, claim.Predicate, claim.Object)
		if key == "" {
			continue
		}
		if seenClaim, ok := currentSeen[key]; ok {
			claim.Subject = strings.TrimSpace(seenClaim.Subject)
			claim.Predicate = strings.TrimSpace(seenClaim.Predicate)
			claim.Object = strings.TrimSpace(seenClaim.Object)
			claim.Salience = clamp(claim.Salience+claimSeenBump, 0, 1)
			claim.LastSeenIn = messageID
		} else {
			claim.Salience = clamp(claim.Salience-claimDecay, 0, 1)
		}
		if claim.Salience < claimPruneThreshold {
			continue
		}
		merged[key] = claim
	}

	for key, claim := range currentSeen {
		if existing, ok := merged[key]; ok {
			existing.Salience = clamp(existing.Salience+claimSeenBump, 0, 1)
			existing.LastSeenIn = messageID
			merged[key] = existing
			continue
		}
		merged[key] = models.ClaimState{
			Subject:    strings.TrimSpace(claim.Subject),
			Predicate:  strings.TrimSpace(claim.Predicate),
			Object:     strings.TrimSpace(claim.Object),
			Salience:   newClaimSalience,
			LastSeenIn: messageID,
		}
	}

	out := make([]models.ClaimState, 0, len(merged))
	for _, claim := range merged {
		out = append(out, claim)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Salience == out[j].Salience {
			return claimKey(out[i].Subject, out[i].Predicate, out[i].Object) < claimKey(out[j].Subject, out[j].Predicate, out[j].Object)
		}
		return out[i].Salience > out[j].Salience
	})
	if len(out) > maxActiveClaims {
		out = out[:maxActiveClaims]
	}
	return out
}

func buildResponseContextArtifact(message models.RawMessage, state models.WorkingState, recentMessages []models.RawMessage) models.ResponseContextArtifact {
	var builder strings.Builder

	builder.WriteString("Current Conversation\n")
	if strings.TrimSpace(state.RollingSummary) == "" {
		builder.WriteString("No rolling summary yet.\n")
	} else {
		builder.WriteString(state.RollingSummary)
		builder.WriteString("\n")
	}

	builder.WriteString("\nActive Topics\n")
	if len(state.ActiveTopics) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, topic := range state.ActiveTopics {
			builder.WriteString("- ")
			builder.WriteString(topic.Name)
			builder.WriteString("\n")
		}
	}

	builder.WriteString("\nRelated Claims\n")
	relevantClaims := topicRelatedClaims(state.ActiveTopics, state.ActiveClaims)
	if len(relevantClaims) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, claim := range relevantClaims {
			builder.WriteString("- ")
			builder.WriteString(claim.Subject)
			builder.WriteString(" | ")
			builder.WriteString(claim.Predicate)
			builder.WriteString(" | ")
			builder.WriteString(claim.Object)
			builder.WriteString("\n")
		}
	}

	builder.WriteString("\nRecent Exchange\n")
	if len(recentMessages) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, recent := range recentMessages {
			builder.WriteString("- ")
			builder.WriteString(renderMessageSpeaker(recent))
			builder.WriteString(": ")
			builder.WriteString(recent.Content)
			builder.WriteString("\n")
		}
	}

	return models.ResponseContextArtifact{
		MessageID:           message.MessageID,
		ConversationID:      message.ConversationID,
		WorkingStateVersion: state.StateVersion,
		Brief:               strings.TrimSpace(builder.String()),
		RecentMessageIDs:    lastMessageIDs(recentMessages),
		CreatedAtUnixMs:     message.TimestampUnixMs,
	}
}

func contentWords(input string) map[string]struct{} {
	fields := strings.FieldsFunc(strings.ToLower(input), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	out := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if len(field) < 3 {
			continue
		}
		if _, skip := stopWords[field]; skip {
			continue
		}
		out[field] = struct{}{}
	}
	return out
}

func topicRelatedClaims(topics []models.TopicState, claims []models.ClaimState) []models.ClaimState {
	if len(topics) == 0 || len(claims) == 0 {
		return nil
	}

	topicNames := make([]string, 0, len(topics))
	topicWords := make(map[string]struct{})
	for _, topic := range topics {
		name := normalizeTopicName(topic.Name)
		if name == "" {
			continue
		}
		topicNames = append(topicNames, name)
		for word := range contentWords(name) {
			topicWords[word] = struct{}{}
		}
	}
	if len(topicNames) == 0 {
		return nil
	}

	out := make([]models.ClaimState, 0, min(len(claims), maxBriefClaims))
	for _, claim := range claims {
		if !claimRelatesToTopics(claim, topicNames, topicWords) {
			continue
		}
		out = append(out, claim)
		if len(out) >= maxBriefClaims {
			break
		}
	}
	return out
}

func claimRelatesToTopics(claim models.ClaimState, topicNames []string, topicWords map[string]struct{}) bool {
	claimText := canonicalText(claim.Subject + " " + claim.Predicate + " " + claim.Object)
	if claimText == "" {
		return false
	}
	for _, topicName := range topicNames {
		if topicName != "" && strings.Contains(claimText, topicName) {
			return true
		}
	}
	for word := range contentWords(claimText) {
		if _, ok := topicWords[word]; ok {
			return true
		}
	}
	return false
}

func appendBoundedUnique(existing []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return trimToLast(existing, limit)
	}
	out := make([]string, 0, len(existing)+1)
	for _, item := range existing {
		item = strings.TrimSpace(item)
		if item == "" || item == value {
			continue
		}
		out = append(out, item)
	}
	out = append(out, value)
	return trimToLast(out, limit)
}

func trimToLast(values []string, limit int) []string {
	if limit <= 0 {
		limit = 1
	}
	if len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[len(values)-limit:]...)
}

func lastMessageIDs(messages []models.RawMessage) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		if strings.TrimSpace(message.MessageID) == "" {
			continue
		}
		out = append(out, message.MessageID)
	}
	return out
}

func renderMessageSpeaker(message models.RawMessage) string {
	if message.AuthorRole == "assistant" {
		return "assistant"
	}
	if strings.TrimSpace(message.AuthorID) != "" {
		return "user " + message.AuthorID
	}
	return message.AuthorRole
}

func normalizeTopicName(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	input = strings.Join(strings.Fields(input), " ")
	input = strings.TrimPrefix(input, "the ")
	return input
}

func topicStatus(salience float64) string {
	if salience < topicActiveThreshold {
		return "fading"
	}
	return "active"
}

func claimKey(subject, predicate, object string) string {
	subject = canonicalText(subject)
	predicate = canonicalText(predicate)
	object = canonicalText(object)
	if subject == "" || predicate == "" || object == "" {
		return ""
	}
	return subject + "|" + predicate + "|" + object
}

func canonicalText(input string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(input)), " "))
}

func clamp(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

var stopWords = map[string]struct{}{
	"about": {}, "after": {}, "again": {}, "also": {}, "been": {}, "could": {}, "from": {}, "have": {}, "into": {}, "just": {},
	"like": {}, "that": {}, "them": {}, "they": {}, "this": {}, "what": {}, "when": {}, "where": {}, "which": {}, "will": {},
	"with": {}, "would": {}, "your": {}, "their": {}, "there": {}, "then": {}, "than": {}, "because": {}, "while": {}, "still": {},
}

func responseContextPayload(artifact models.ResponseContextArtifact) map[string]any {
	return map[string]any{
		"message_id":            artifact.MessageID,
		"conversation_id":       artifact.ConversationID,
		"working_state_version": artifact.WorkingStateVersion,
		"brief":                 artifact.Brief,
		"recent_message_ids":    artifact.RecentMessageIDs,
		"created_at_unix_ms":    artifact.CreatedAtUnixMs,
	}
}

func workingStatePayload(state models.WorkingState) map[string]any {
	return map[string]any{
		"conversation_id":           state.ConversationID,
		"last_updated_message_id":   state.LastUpdatedMessageID,
		"state_version":             state.StateVersion,
		"rolling_summary":           state.RollingSummary,
		"active_topics":             state.ActiveTopics,
		"active_claims":             state.ActiveClaims,
		"recent_message_ids":        state.RecentMessageIDs,
		"recent_extraction_ids":     state.RecentExtractionIDs,
		"last_compacted_at_message": state.LastCompactedAtMessage,
	}
}

func messageExtractionPayload(extraction models.MessageExtraction) map[string]any {
	return map[string]any{
		"message_id":            extraction.MessageID,
		"conversation_id":       extraction.ConversationID,
		"claims":                extraction.Claims,
		"active_topics":         extraction.ActiveTopics,
		"message_summary":       extraction.MessageSummary,
		"claims_status":         extraction.ClaimsStatus,
		"topics_status":         extraction.TopicsStatus,
		"summary_status":        extraction.SummaryStatus,
		"claims_model_version":  extraction.ClaimsModelVersion,
		"topics_model_version":  extraction.TopicsModelVersion,
		"summary_model_version": extraction.SummaryModelVersion,
	}
}

func messagePayload(message models.RawMessage) map[string]any {
	return map[string]any{
		"message_id":          message.MessageID,
		"conversation_id":     message.ConversationID,
		"sequence_number":     message.SequenceNumber,
		"author_id":           message.AuthorID,
		"author_role":         message.AuthorRole,
		"content":             message.Content,
		"timestamp_unix_ms":   message.TimestampUnixMs,
		"reply_to_message_id": message.ReplyToMessageID,
	}
}

func extractorStatusesPayload(extraction models.MessageExtraction) map[string]any {
	return map[string]any{
		"claims":  extraction.ClaimsStatus,
		"topics":  extraction.TopicsStatus,
		"summary": extraction.SummaryStatus,
	}
}

func statusCounts(extraction models.MessageExtraction) map[string]int {
	return map[string]int{
		"claims":     len(extraction.Claims),
		"topics":     len(extraction.ActiveTopics),
		"summary_ok": boolToInt(strings.TrimSpace(extraction.MessageSummary) != ""),
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func summaryPreview(input string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(input))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func formatWorkingStateCounts(state models.WorkingState) map[string]int {
	return map[string]int{
		"active_topics":   len(state.ActiveTopics),
		"active_claims":   len(state.ActiveClaims),
		"recent_messages": len(state.RecentMessageIDs),
	}
}
