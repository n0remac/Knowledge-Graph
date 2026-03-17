package conversation

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/n0remac/Knowledge-Graph/internal/models"
)

const (
	maxPromptRecentMessages    = 5
	maxRecentStateEntries      = 10
	maxResponseRecentMessages  = 4
	maxActiveTopics            = 6
	maxOpenQuestions           = 5
	maxActiveClaims            = 8
	maxPronounBindings         = 8
	newTopicSalience           = 0.75
	topicSeenBump              = 0.20
	topicDecay                 = 0.10
	topicActiveThreshold       = 0.45
	topicPruneThreshold        = 0.20
	newQuestionSalience        = 1.00
	questionSeenBump           = 0.15
	questionDecay              = 0.08
	questionPruneThreshold     = 0.20
	newClaimSalience           = 0.70
	claimSeenBump              = 0.15
	claimDecay                 = 0.07
	claimPruneThreshold        = 0.25
	minPronounConfidence       = 0.60
	minPronounKeepConfidence   = 0.50
	pronounConfidenceDecay     = 0.05
	pronounExpiryMessageWindow = 3
)

func mergeWorkingState(previous models.WorkingState, current models.RawMessage, extraction models.MessageExtraction, rollingSummary string, sequenceForMessage func(string) int64) models.WorkingState {
	state := previous
	state.ConversationID = current.ConversationID
	state.LastUpdatedMessageID = current.MessageID
	state.StateVersion = previous.StateVersion + 1
	if strings.TrimSpace(rollingSummary) != "" {
		state.RollingSummary = strings.TrimSpace(rollingSummary)
	}

	state.ActiveTopics = mergeTopics(previous.ActiveTopics, extraction.ActiveTopics, current.MessageID)
	state.OpenQuestions = mergeOpenQuestions(previous.OpenQuestions, extraction.OpenQuestions, extraction.Claims, current, sequenceForMessage)
	state.ActiveClaims = mergeClaims(previous.ActiveClaims, extraction.Claims, current.MessageID)
	state.PronounResolutionMap = mergePronouns(previous.PronounResolutionMap, extraction.PronounResolutions, current.MessageID, current.SequenceNumber, sequenceForMessage)
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

func mergeOpenQuestions(previous []models.OpenQuestionState, current []models.OpenQuestion, currentClaims []models.Claim, message models.RawMessage, sequenceForMessage func(string) int64) []models.OpenQuestionState {
	currentSeen := make(map[string]models.OpenQuestion, len(current))
	for _, question := range current {
		text := normalizeQuestion(question.Text)
		if text == "" {
			continue
		}
		question.Text = text
		currentSeen[text] = question
	}

	isQuestionMessage := isLikelyQuestion(message.Content)
	merged := make(map[string]models.OpenQuestionState, len(previous)+len(currentSeen))
	for _, question := range previous {
		text := normalizeQuestion(question.Text)
		if text == "" {
			continue
		}
		question.Text = text
		if currentQuestion, seen := currentSeen[text]; seen {
			question.Status = "open"
			question.Salience = clamp(question.Salience+questionSeenBump, 0, 1)
			question.LastSeenIn = currentQuestion.SourceMessageID
			merged[text] = question
			continue
		}

		question.Salience = clamp(question.Salience-questionDecay, 0, 1)
		if question.Status == "open" && !isQuestionMessage && sharesMeaningfulWords(question.Text, message.Content, currentClaims) {
			question.Status = "resolved"
			question.LastSeenIn = message.MessageID
		}

		if shouldPruneQuestion(question, message.SequenceNumber, sequenceForMessage) {
			continue
		}
		merged[text] = question
	}

	for text, question := range currentSeen {
		if _, ok := merged[text]; ok {
			continue
		}
		merged[text] = models.OpenQuestionState{
			Text:       question.Text,
			Status:     "open",
			Salience:   newQuestionSalience,
			LastSeenIn: question.SourceMessageID,
		}
	}

	out := make([]models.OpenQuestionState, 0, len(merged))
	for _, question := range merged {
		if question.Salience < questionPruneThreshold {
			continue
		}
		out = append(out, question)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status == out[j].Status {
			if out[i].Salience == out[j].Salience {
				return out[i].Text < out[j].Text
			}
			return out[i].Salience > out[j].Salience
		}
		return out[i].Status < out[j].Status
	})
	if len(out) > maxOpenQuestions {
		out = out[:maxOpenQuestions]
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

func mergePronouns(previous []models.PronounBinding, current []models.PronounResolution, messageID string, sequenceNumber int64, sequenceForMessage func(string) int64) []models.PronounBinding {
	currentSeen := make(map[string]models.PronounResolution, len(current))
	for _, binding := range current {
		expression := normalizeExpression(binding.Expression)
		if expression == "" || strings.TrimSpace(binding.Referent) == "" || binding.Confidence < minPronounConfidence {
			continue
		}
		binding.Expression = expression
		currentSeen[expression] = binding
	}

	merged := make(map[string]models.PronounBinding, len(previous)+len(currentSeen))
	for _, binding := range previous {
		expression := normalizeExpression(binding.Expression)
		if expression == "" {
			continue
		}
		binding.Expression = expression
		if currentBinding, seen := currentSeen[expression]; seen {
			if currentBinding.Confidence >= binding.Confidence || !strings.EqualFold(strings.TrimSpace(currentBinding.Referent), strings.TrimSpace(binding.Referent)) {
				binding.Referent = strings.TrimSpace(currentBinding.Referent)
				binding.Confidence = clamp(currentBinding.Confidence, 0, 1)
			}
			binding.LastSeenIn = messageID
			merged[expression] = binding
			continue
		}

		binding.Confidence = clamp(binding.Confidence-pronounConfidenceDecay, 0, 1)
		if sequenceNumber-sequenceForMessage(binding.LastSeenIn) >= pronounExpiryMessageWindow || binding.Confidence < minPronounKeepConfidence {
			continue
		}
		merged[expression] = binding
	}

	for expression, binding := range currentSeen {
		if existing, ok := merged[expression]; ok {
			if binding.Confidence >= existing.Confidence || !strings.EqualFold(strings.TrimSpace(binding.Referent), strings.TrimSpace(existing.Referent)) {
				existing.Referent = strings.TrimSpace(binding.Referent)
				existing.Confidence = clamp(binding.Confidence, 0, 1)
			}
			existing.LastSeenIn = messageID
			merged[expression] = existing
			continue
		}
		merged[expression] = models.PronounBinding{
			Expression: expression,
			Referent:   strings.TrimSpace(binding.Referent),
			Confidence: clamp(binding.Confidence, 0, 1),
			LastSeenIn: messageID,
		}
	}

	out := make([]models.PronounBinding, 0, len(merged))
	for _, binding := range merged {
		out = append(out, binding)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence == out[j].Confidence {
			return out[i].Expression < out[j].Expression
		}
		return out[i].Confidence > out[j].Confidence
	})
	if len(out) > maxPronounBindings {
		out = out[:maxPronounBindings]
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

	builder.WriteString("\nOpen Questions\n")
	openCount := 0
	for _, question := range state.OpenQuestions {
		if question.Status != "open" {
			continue
		}
		openCount++
		builder.WriteString("- ")
		builder.WriteString(question.Text)
		builder.WriteString("\n")
	}
	if openCount == 0 {
		builder.WriteString("- none\n")
	}

	builder.WriteString("\nCurrent References\n")
	if len(state.PronounResolutionMap) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, binding := range state.PronounResolutionMap {
			builder.WriteString("- ")
			builder.WriteString(binding.Expression)
			builder.WriteString(" -> ")
			builder.WriteString(binding.Referent)
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

func shouldPruneQuestion(question models.OpenQuestionState, currentSequence int64, sequenceForMessage func(string) int64) bool {
	if question.Salience < questionPruneThreshold {
		return true
	}
	if question.Status != "resolved" {
		return false
	}
	return currentSequence-sequenceForMessage(question.LastSeenIn) >= 2
}

func sharesMeaningfulWords(questionText, messageContent string, claims []models.Claim) bool {
	questionWords := contentWords(questionText)
	if len(questionWords) == 0 {
		return false
	}
	messageWords := contentWords(messageContent)
	for _, claim := range claims {
		for word := range contentWords(claim.Subject + " " + claim.Predicate + " " + claim.Object) {
			messageWords[word] = struct{}{}
		}
	}

	shared := 0
	for word := range questionWords {
		if _, ok := messageWords[word]; ok {
			shared++
		}
	}
	return shared >= 2
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

func normalizeQuestion(input string) string {
	input = strings.TrimSpace(input)
	input = strings.Join(strings.Fields(input), " ")
	if input == "" {
		return ""
	}
	return input
}

func normalizeExpression(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	input = strings.Join(strings.Fields(input), " ")
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

func isLikelyQuestion(input string) bool {
	input = strings.TrimSpace(input)
	if input == "" {
		return false
	}
	if strings.Contains(input, "?") {
		return true
	}
	lower := strings.ToLower(input)
	prefixes := []string{"what ", "why ", "how ", "when ", "where ", "who ", "can ", "should ", "do ", "does ", "did ", "is ", "are ", "will "}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
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
		"open_questions":            state.OpenQuestions,
		"active_claims":             state.ActiveClaims,
		"pronoun_resolution_map":    state.PronounResolutionMap,
		"recent_message_ids":        state.RecentMessageIDs,
		"recent_extraction_ids":     state.RecentExtractionIDs,
		"last_compacted_at_message": state.LastCompactedAtMessage,
	}
}

func messageExtractionPayload(extraction models.MessageExtraction) map[string]any {
	return map[string]any{
		"message_id":              extraction.MessageID,
		"conversation_id":         extraction.ConversationID,
		"claims":                  extraction.Claims,
		"open_questions":          extraction.OpenQuestions,
		"active_topics":           extraction.ActiveTopics,
		"pronoun_resolutions":     extraction.PronounResolutions,
		"message_summary":         extraction.MessageSummary,
		"claims_status":           extraction.ClaimsStatus,
		"questions_status":        extraction.QuestionsStatus,
		"topics_status":           extraction.TopicsStatus,
		"pronouns_status":         extraction.PronounsStatus,
		"summary_status":          extraction.SummaryStatus,
		"claims_model_version":    extraction.ClaimsModelVersion,
		"questions_model_version": extraction.QuestionsModelVersion,
		"topics_model_version":    extraction.TopicsModelVersion,
		"pronouns_model_version":  extraction.PronounsModelVersion,
		"summary_model_version":   extraction.SummaryModelVersion,
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
		"claims":    extraction.ClaimsStatus,
		"questions": extraction.QuestionsStatus,
		"topics":    extraction.TopicsStatus,
		"pronouns":  extraction.PronounsStatus,
		"summary":   extraction.SummaryStatus,
	}
}

func statusCounts(extraction models.MessageExtraction) map[string]int {
	return map[string]int{
		"claims":     len(extraction.Claims),
		"questions":  len(extraction.OpenQuestions),
		"topics":     len(extraction.ActiveTopics),
		"pronouns":   len(extraction.PronounResolutions),
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
	openQuestions := 0
	for _, question := range state.OpenQuestions {
		if question.Status == "open" {
			openQuestions++
		}
	}
	return map[string]int{
		"active_topics":    len(state.ActiveTopics),
		"open_questions":   openQuestions,
		"active_claims":    len(state.ActiveClaims),
		"pronoun_bindings": len(state.PronounResolutionMap),
		"recent_messages":  len(state.RecentMessageIDs),
	}
}

func conversationTitle(snapshot models.WorkingState) string {
	return fmt.Sprintf("conversation %s", snapshot.ConversationID)
}
