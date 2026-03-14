package telemetry

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type discordSender interface {
	ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error)
	GuildChannels(guildID string, options ...discordgo.RequestOption) ([]*discordgo.Channel, error)
	StateGuilds() []*discordgo.Guild
}

type DiscordReporter struct {
	cfg               Config
	sender            discordSender
	reportCh          chan WrittenEvent
	emit              func(context.Context, string, string, string, map[string]any)
	resolvedChannelID string
}

type sessionDiscordSender struct {
	session *discordgo.Session
}

func newSessionDiscordSender(session *discordgo.Session) *sessionDiscordSender {
	if session == nil {
		return nil
	}
	return &sessionDiscordSender{session: session}
}

func (s *sessionDiscordSender) ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error) {
	return s.session.ChannelMessageSendComplex(channelID, data, options...)
}

func (s *sessionDiscordSender) GuildChannels(guildID string, options ...discordgo.RequestOption) ([]*discordgo.Channel, error) {
	return s.session.GuildChannels(guildID, options...)
}

func (s *sessionDiscordSender) StateGuilds() []*discordgo.Guild {
	if s == nil || s.session == nil || s.session.State == nil {
		return nil
	}
	return s.session.State.Guilds
}

func newDiscordReporter(cfg Config, sender discordSender, emit func(context.Context, string, string, string, map[string]any)) *DiscordReporter {
	if sender == nil {
		return nil
	}
	return &DiscordReporter{
		cfg:      cfg,
		sender:   sender,
		reportCh: make(chan WrittenEvent, cfg.BufferSize),
		emit:     emit,
	}
}

func (r *DiscordReporter) run() {
	for written := range r.reportCh {
		r.processWrittenEvent(written)
	}
}

func (r *DiscordReporter) processWrittenEvent(written WrittenEvent) {
	report, ok := r.buildReportItem(written)
	if !ok {
		return
	}

	files, closers := r.buildDiscordFiles(report.AttachmentPaths)
	for _, closer := range closers {
		defer closer()
	}

	message := &discordgo.MessageSend{
		Content: report.Content,
		Files:   files,
	}
	channelID, err := r.targetChannelID()
	if err != nil {
		log.Printf("telemetry discord channel resolution failed trace_id=%q kind=%q err=%q", written.Event.TraceID, written.Event.Kind, err.Error())
		if r.emit != nil {
			r.emit(
				WithTraceID(context.Background(), written.Event.TraceID),
				StageDiscordDebug,
				"delivery_error",
				"discord telemetry channel resolution failed",
				map[string]any{
					"error":          err.Error(),
					"original_kind":  written.Event.Kind,
					"original_stage": written.Event.Stage,
					"report_kind":    report.Kind,
					"channel_target": r.cfg.DiscordDebugChannelID,
				},
			)
		}
		return
	}
	if _, err := r.sender.ChannelMessageSendComplex(channelID, message); err != nil {
		log.Printf("telemetry discord delivery failed trace_id=%q kind=%q err=%q", written.Event.TraceID, written.Event.Kind, err.Error())
		if r.emit != nil {
			r.emit(
				WithTraceID(context.Background(), written.Event.TraceID),
				StageDiscordDebug,
				"delivery_error",
				"discord telemetry delivery failed",
				map[string]any{
					"error":          err.Error(),
					"original_kind":  written.Event.Kind,
					"original_stage": written.Event.Stage,
					"report_kind":    report.Kind,
					"channel_target": r.cfg.DiscordDebugChannelID,
					"channel_id":     channelID,
				},
			)
		}
	}
}

func (r *DiscordReporter) buildDiscordFiles(paths []string) ([]*discordgo.File, []func()) {
	if len(paths) == 0 {
		return nil, nil
	}

	files := make([]*discordgo.File, 0, len(paths))
	closers := make([]func(), 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		files = append(files, &discordgo.File{
			Name:   filepath.Base(path),
			Reader: bytes.NewReader(raw),
		})
		closers = append(closers, func() {})
	}
	return files, closers
}

func (r *DiscordReporter) buildReportItem(written WrittenEvent) (ReportItem, bool) {
	if r == nil || !r.cfg.EnableDiscordReporting || strings.TrimSpace(r.cfg.DiscordDebugChannelID) == "" {
		return ReportItem{}, false
	}
	if written.Event.Stage == StageDiscordDebug {
		return ReportItem{}, false
	}

	content, ok := r.reportContent(written)
	if !ok {
		return ReportItem{}, false
	}

	report := ReportItem{
		TraceID:  written.Event.TraceID,
		Sequence: written.Event.Sequence,
		Kind:     written.Event.Kind,
		Content:  content,
	}
	report.AttachmentPaths = r.attachmentsForEvent(written)
	return report, true
}

func (r *DiscordReporter) reportContent(written WrittenEvent) (string, bool) {
	event := written.Event
	summary := written.Summary

	switch event.Kind {
	case "message_received":
		source := summary.SourceMessage
		return fmt.Sprintf(
			"trace `%s`\nauthor: `%v`\nchannel: `%v`\nstatus: `%s`\nmessage: %q",
			event.TraceID,
			source["author_id"],
			source["channel_id"],
			written.TraceIndex.Status,
			source["content"],
		), true
	case "retrieve_for_extraction", "retrieve_for_generation":
		counts := payloadMap(event.Payload, "counts")
		return fmt.Sprintf("trace `%s`\n%s\ncounts: %v", event.TraceID, event.Summary, counts), true
	case "extract_topics_result":
		return fmt.Sprintf("trace `%s`\n%s\ntopics: %v", event.TraceID, event.Summary, summary.TopicCandidates), true
	case "extract_facts_result":
		return fmt.Sprintf("trace `%s`\n%s\nfacts: %v", event.TraceID, event.Summary, summary.FactCandidates), true
	case "ollama_request", "ollama_response":
		return fmt.Sprintf("trace `%s`\n%s\npurpose: %v", event.TraceID, event.Summary, event.Payload["purpose"]), true
	case "topics_resolved":
		return fmt.Sprintf("trace `%s`\n%s\nresolved_topics: %v", event.TraceID, event.Summary, summary.ResolvedTopics), true
	case "facts_resolved":
		return fmt.Sprintf("trace `%s`\n%s\npersisted_facts: %v", event.TraceID, event.Summary, summary.PersistedFacts), true
	case "generate_reply_result", "reply_generated":
		return fmt.Sprintf("trace `%s`\n%s\nreply: %v", event.TraceID, event.Summary, summary.Reply), true
	case "reply_sent":
		return fmt.Sprintf("trace `%s`\n%s\nstatus: `%s`", event.TraceID, event.Summary, summary.Status), true
	default:
		if isErrorEvent(event) {
			return fmt.Sprintf("trace `%s`\nerror: %s\n%s", event.TraceID, payloadString(event.Payload, "error"), event.Summary), true
		}
		return "", false
	}
}

func (r *DiscordReporter) attachmentsForEvent(written WrittenEvent) []string {
	event := written.Event
	attachments := make([]string, 0, 2)
	if allowAttachment(event.Kind) && withinAttachmentLimit(written.FilePath, r.cfg.MaxAttachmentBytes) {
		attachments = append(attachments, written.FilePath)
	}

	if (event.Kind == "reply_sent" || isErrorEvent(event)) && withinAttachmentLimit(written.SummaryPath, r.cfg.MaxAttachmentBytes) {
		attachments = append(attachments, written.SummaryPath)
	}
	return attachments
}

func allowAttachment(kind string) bool {
	switch kind {
	case "retrieve_for_extraction",
		"retrieve_for_generation",
		"ollama_request",
		"ollama_response",
		"facts_resolved",
		"generate_reply_result",
		"reply_sent":
		return true
	default:
		return false
	}
}

func withinAttachmentLimit(path string, limit int) bool {
	if strings.TrimSpace(path) == "" || limit <= 0 {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() <= int64(limit)
}

func (r *DiscordReporter) targetChannelID() (string, error) {
	target := strings.TrimSpace(r.cfg.DiscordDebugChannelID)
	if target == "" {
		return "", fmt.Errorf("discord debug channel target is empty")
	}
	if isSnowflake(target) {
		return target, nil
	}
	if r.resolvedChannelID != "" {
		return r.resolvedChannelID, nil
	}

	for _, guild := range r.sender.StateGuilds() {
		if guild == nil || strings.TrimSpace(guild.ID) == "" {
			continue
		}
		channels, err := r.sender.GuildChannels(guild.ID)
		if err != nil {
			continue
		}
		for _, channel := range channels {
			if channel == nil {
				continue
			}
			if !isReportableGuildChannel(channel.Type) {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(channel.Name), target) {
				r.resolvedChannelID = channel.ID
				return channel.ID, nil
			}
		}
	}

	return "", fmt.Errorf("no accessible discord channel named %q found", target)
}

func isSnowflake(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isReportableGuildChannel(channelType discordgo.ChannelType) bool {
	switch channelType {
	case discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews:
		return true
	default:
		return false
	}
}
