# Knowledge-Graph

Discord bot with a live conversation-state memory layer.

## Current behavior

For each non-bot message the bot can read:
1. Persist the raw message.
2. Run parallel per-message extraction for claims, questions, topics, pronouns, and short summary.
3. Update a rolling working state for the active conversation.
4. Build a compact readable response brief from the working state.
5. Generate a grounded reply using that brief.
6. Persist the assistant reply back into the same live conversation state.

## Setup

### Required

- `DISCORD_BOT_TOKEN`

### Optional

- `OLLAMA_BASE_URL` (default: `http://localhost:11434`)
- `OLLAMA_CHAT_MODEL` (default: `qwen2.5:1.5b-instruct`)
- `OLLAMA_EXTRACT_MODEL` (default: value of `OLLAMA_CHAT_MODEL`)
- `BOT_PERSONA` (default: `You are a helpful Discord assistant.`)
- `CONVERSATION_STORE_PATH` (default: `data/conversation-state.json`)
- `WEB_ADDR` (default: `127.0.0.1:8080`)
- `REQUEST_TIMEOUT_SECONDS` (default: `45`)

### Discord app settings

- Enable `Message Content Intent`.
- Invite the bot with:
  - `View Channels`
  - `Read Message History`
  - `Send Messages`

## Run

```bash
export DISCORD_BOT_TOKEN=your_bot_token
export OLLAMA_BASE_URL=http://localhost:11434
export OLLAMA_CHAT_MODEL=qwen2.5:1.5b-instruct
go run .
```

The live conversation-state viewer is available at `http://127.0.0.1:8080/conversation` by default.

## Data model (current stage)

Live conversation-state entities:
- raw messages
- per-message extraction artifacts
- rolling working state
- response context artifacts
