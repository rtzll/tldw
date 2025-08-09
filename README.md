# tldw - too long; didn't watch

Transform YouTube videos and playlists into concise summaries using AI. Works
with existing captions (free) or Whisper transcription (paid). Includes MCP
server for Claude and other AI assistants and CLI.

## Installation

```bash
brew install rtzll/tap/tldw
export OPENAI_API_KEY="your-api-key-here"  # optional: for AI summaries & Whisper
```

## MCP

`tldw mcp` provides AI assistants with:

- **`get_youtube_metadata`**: Video or playlist metadata and captions status
- **`get_youtube_transcript`**: Free captions transcript (supports playlists)
- **`transcribe_youtube_whisper`**: Paid Whisper transcription (supports
  playlists)

### Claude Desktop Setup

**Easy setup:**

```bash
tldw mcp setup-claude
```

This automatically configures Claude Desktop to use tldw. Restart Claude Desktop
afterward.

**After setup**, ask Claude: _"tldw: https://youtu.be/tAP1eZYEuKA"_

![Claude using tldw via MCP](./assets/claude-tldw-screenshot.png)

## ChatGPT Desktop (not yet)

ChatGPT Desktop will support MCP servers in the
[coming months](https://x.com/OpenAIDevs/status/1904957755829481737).

## CLI

### Usage Examples

#### Single Videos

```bash
# Get transcript (free with captions)
tldw transcribe "https://youtu.be/tAP1eZYEuKA"
tldw transcribe tAP1eZYEuKA -o transcript.txt  # Save to file
tldw transcribe tAP1eZYEuKA --fallback-whisper # Use Whisper if video has no captions

# Generate summary (requires API key)
tldw "https://youtu.be/tAP1eZYEuKA"
tldw tAP1eZYEuKA -m gpt-4o-mini -p "tldr: {{.Transcript}}"

# Get video metadata
tldw metadata "https://youtu.be/tAP1eZYEuKA"
tldw metadata tAP1eZYEuKA -o metadata.json   # Save to file
tldw metadata tAP1eZYEuKA --pretty           # Format JSON output
```

#### Playlists

```bash
# Summarize entire playlist
tldw "https://youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq"
tldw "https://youtu.be/tAP1eZYEuKA?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq" # Video from playlist

# Get playlist transcripts
tldw transcribe "https://youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq"
tldw transcribe "https://youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq" --fallback-whisper

# Get playlist metadata
tldw metadata "https://youtube.com/playlist?list=PLSE8ODhjZXjYDBpQnSymaectKjxCy6BYq"
```

**Note:** Playlist processing shows progress and allows per-video Whisper
confirmation for videos without captions.

### Example Output

Summaries are shown as markdown and rendered in the terminal.

![CLI usage of tldw](./assets/cli-tldw-screenshot.png)

## Configuration

Either edit the config file or use environment variables.

### Config file

**Find your config location:**

```bash
tldw paths  # Shows config, data, and cache directories
```

**Edit config file: `config.toml`**

```toml
openai_api_key = "your-key"
tldr_model = "gpt-5-nano"
prompt = "tldr: {{.Transcript}}"
```

or edit the `prompt.txt` file in the config directory to change the default
summary prompt.

### Environment variables

```bash
export OPENAI_API_KEY="your-key"
export TLDW_TLDR_MODEL="gpt-5-nano"
export TLDW_PROMPT="tldr: {{.Transcript}}"
```
