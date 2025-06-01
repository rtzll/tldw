# tldw - too long; didn't watch

Transform YouTube videos into concise summaries using AI. Works with existing captions (free) or Whisper transcription (paid). Includes MCP server for Claude and other AI assistants.

## Quick Setup

```bash
# Install dependencies
brew install yt-dlp ffmpeg
# Clone and build tldw
git clone https://github.com/rtzll/tldw.git && cd tldw && go build -o tldw
# [Optional] Set OpenAI API key for summaries/whisper
export OPENAI_API_KEY="your-api-key-here"
```

## Claude Desktop Setup

**Easy setup:**
```bash
./tldw mcp setup-claude
```

This automatically configures Claude Desktop to use tldw. Restart Claude Desktop afterward.

**After setup**, ask Claude: *"tldw: https://youtu.be/tAP1eZYEuKA"*

![Claude using tldw via MCP](./assets/claude-tldw-screenshot.png)

## ChatGPT Desktop (not yet)

ChatGPT Desktop will support MCP servers in the [coming months](https://x.com/OpenAIDevs/status/1904957755829481737).

## CLI Usage Examples

```bash
# Get transcript (free with captions)
./tldw transcribe "https://youtu.be/tAP1eZYEuKA"
./tldw transcribe tAP1eZYEuKA -o transcript.txt  # Save to file
./tldw transcribe tAP1eZYEuKA --fallback-whisper # Use Whisper if no captions

# Generate summary (requires API key)
./tldw "https://youtu.be/tAP1eZYEuKA"
./tldw tAP1eZYEuKA --model gpt-4o --prompt "tldr: {{.Transcript}}"

# Start MCP server
./tldw mcp
```

### Example Output

![CLI usage of tldw](./assets/cli-tldw-screenshot.png)

## How It Works

- **Free transcripts**: Uses YouTube captions when available
- **Paid transcription**: Whisper via OpenAI API as a fallback ($0.006/min)
- **AI summaries**: Configurable OpenAI models
- **Caching**: Local transcript storage
- **MCP integration**: Tools for AI assistants

## MCP Tools

`./tldw mcp` provides AI assistants with:
- **`get_youtube_metadata`**: Video info and captions status
- **`get_youtube_transcript`**: Free captions transcript
- **`transcribe_youtube_whisper`**: Paid Whisper transcription

## Commands

```bash
./tldw [url]                      # Summarize video (requires API key)
./tldw transcribe [url] [-o file] # Get transcript from captions
./tldw summarize [url]            # Generate summary (requires API key)
./tldw mcp [--port 8080]          # Start MCP server
./tldw paths                      # Show config locations
./tldw version                    # Show version
```

## Configuration

**Find your config location:**
```bash
./tldw paths  # Shows config, data, and cache directories
```

**Edit config file: `config.toml`**
```toml
openai_api_key = "your-key"
tldr_model = "gpt-4o-mini"
prompt = "tldr: {{.Transcript}}"
```

or edit the `prompt.txt` file in the config directory to change the default summary prompt.
