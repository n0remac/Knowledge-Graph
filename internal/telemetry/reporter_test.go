package telemetry

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

type fakeDiscordSender struct {
	channelID string
	message   *discordgo.MessageSend
	calls     int
	channels  map[string][]*discordgo.Channel
	guilds    []*discordgo.Guild
}

func (f *fakeDiscordSender) ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	f.channelID = channelID
	f.message = data
	f.calls++
	return &discordgo.Message{}, nil
}

func (f *fakeDiscordSender) GuildChannels(guildID string, _ ...discordgo.RequestOption) ([]*discordgo.Channel, error) {
	return f.channels[guildID], nil
}

func (f *fakeDiscordSender) StateGuilds() []*discordgo.Guild {
	return f.guilds
}

func TestReporterBuildsMessageAndAttachments(t *testing.T) {
	dir := t.TempDir()
	filePath := traceDirectory(dir, "trace-1")
	eventPath := filePath + "/001_extraction_ollama_request.json"
	summaryPath := filePath + "/summary.json"

	if err := writeJSONAtomic(eventPath, map[string]any{"ok": true}); err != nil {
		t.Fatalf("write event file: %v", err)
	}
	if err := writeJSONAtomic(summaryPath, map[string]any{"ok": true}); err != nil {
		t.Fatalf("write summary file: %v", err)
	}

	sender := &fakeDiscordSender{}
	reporter := newDiscordReporter(Config{
		EnableDiscordReporting: true,
		DiscordDebugChannelID:  "123456789012345678",
		MaxAttachmentBytes:     1024,
	}, sender, nil)

	reporter.processWrittenEvent(WrittenEvent{
		Event: Event{
			TraceID:  "trace-1",
			Sequence: 1,
			Stage:    StageExtraction,
			Kind:     "ollama_request",
			Summary:  "request",
		},
		FilePath:    eventPath,
		SummaryPath: summaryPath,
		Summary: TraceSummary{
			TraceID: "trace-1",
			Status:  "in_progress",
		},
		TraceIndex: TraceIndex{
			TraceID: "trace-1",
			Status:  "in_progress",
		},
	})

	if sender.calls != 1 {
		t.Fatalf("calls = %d, want 1", sender.calls)
	}
	if sender.channelID != "123456789012345678" {
		t.Fatalf("channelID = %q", sender.channelID)
	}
	if len(sender.message.Files) != 1 {
		t.Fatalf("attachments = %d, want 1", len(sender.message.Files))
	}
}

func TestReporterResolvesChannelName(t *testing.T) {
	dir := t.TempDir()
	traceDir := traceDirectory(dir, "trace-2")
	eventPath := traceDir + "/001_runtime_message_received.json"

	if err := writeJSONAtomic(eventPath, map[string]any{"ok": true}); err != nil {
		t.Fatalf("write event file: %v", err)
	}

	sender := &fakeDiscordSender{
		channels: map[string][]*discordgo.Channel{
			"guild-1": {
				{ID: "123456789012345678", Name: "bot-debug", Type: discordgo.ChannelTypeGuildText},
			},
		},
		guilds: []*discordgo.Guild{
			{ID: "guild-1", Name: "Test Guild"},
		},
	}

	reporter := newDiscordReporter(Config{
		EnableDiscordReporting: true,
		DiscordDebugChannelID:  "bot-debug",
		MaxAttachmentBytes:     1024,
	}, sender, nil)

	reporter.processWrittenEvent(WrittenEvent{
		Event: Event{
			TraceID:  "trace-2",
			Sequence: 1,
			Stage:    StageRuntime,
			Kind:     "message_received",
			Summary:  "received",
		},
		FilePath: eventPath,
		Summary: TraceSummary{
			TraceID: "trace-2",
			Status:  "in_progress",
			SourceMessage: map[string]any{
				"author_id":  "user-1",
				"channel_id": "source-channel",
				"content":    "hello",
			},
		},
		TraceIndex: TraceIndex{
			TraceID: "trace-2",
			Status:  "in_progress",
		},
	})

	if sender.calls != 1 {
		t.Fatalf("calls = %d, want 1", sender.calls)
	}
	if sender.channelID != "123456789012345678" {
		t.Fatalf("channelID = %q", sender.channelID)
	}
}
