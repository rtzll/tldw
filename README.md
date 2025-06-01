# TLDW - too long; didn't watch

Transform YouTube videos into concise summaries using AI. Works with existing captions (free) or Whisper transcription (paid). Includes MCP server for Claude and other AI assistants.

## Quick Setup

**1. Install dependencies:**
```bash
# macOS
brew install yt-dlp ffmpeg

# Ubuntu/Debian
sudo apt install yt-dlp ffmpeg
```

**2. Build TLDW:**
```bash
git clone https://github.com/rtzll/tldw.git
cd tldw
go build -o tldw
```

**3. Set OpenAI API key** (for summaries or whisper transcription):
```bash
export OPENAI_API_KEY="your-api-key-here"
```

## Usage Examples

```bash
# Get transcript only (free if video has captions)
./tldw transcribe "https://www.youtube.com/watch?v=tAP1eZYEuKA"

# Save transcript to file
./tldw transcribe "https://www.youtube.com/watch?v=tAP1eZYEuKA" -o transcript.txt

# Generate summary (requires API key)
./tldw "https://www.youtube.com/watch?v=tAP1eZYEuKA"

# Use with video ID instead of full URL
./tldw transcribe tAP1eZYEuKA

# Try captions first, fallback to Whisper if none available (costs money only if no captions available)
./tldw transcribe tAP1eZYEuKA --fallback-whisper

# Custom model and prompt
./tldw "youtube-url" --model gpt-4o --prompt "tldr: {{.Transcript}}"

# Start MCP server for AI assistants
./tldw mcp
```

## Claude Desktop Setup

**Easy setup:**
```bash
./tldw mcp setup-claude
```

This automatically configures Claude Desktop to use TLDW. Restart Claude Desktop afterward.

**After setup**, ask Claude: *"tldw: https://youtu.be/tAP1eZYEuKA"*

## ChatGPT Desktop (not yet)

ChatGPT Desktop does not support MCP servers yet. But it was announced that it will support them in the [comming months](https://x.com/OpenAIDevs/status/1904957755829481737). 


## How It Works

1. **Free transcripts**: Uses YouTube's existing captions when available
2. **Paid transcription**: Downloads audio and uses Whisper API as fallback ($0.006/minute), if too large splits into chunks, processes them separately and combines them
3. **AI summaries**: Processes transcripts with configurable OpenAI models
4. **Smart caching**: Saves transcripts locally to avoid reprocessing
5. **MCP integration**: Exposes tools for Claude and other AI assistants

## MCP Tools

When running `./tldw mcp`, AI assistants get access to:

- **`get_youtube_metadata`** - Video info, duration, has captions indicator
- **`get_youtube_transcript`** - Free captions-only transcript
- **`transcribe_youtube_whisper`** - Paid Whisper transcription (requires user consent)

## Commands

```bash
./tldw [url]                                # Summarize video (requires API key)
./tldw transcribe [url]                     # Get transcript from captions (if available), print to stdout
./tldw transcribe [url] -o file.txt         # Save transcript to file
./tldw transcribe [url] --fallback-whisper  # Try captions, use Whisper if none available (costs money)
./tldw summarize [url]                      # Generate summary (requires API key)
./tldw mcp [--port 8080]                    # Start MCP server
./tldw paths                                # Show config/data directories
./tldw version                              # Show version info
```

## Configuration

**Find your config location:**
```bash
./tldw paths  # Shows config, data, and cache directories
```

**Edit config file:**
```toml
openai_api_key = "your-key"
tldr_model = "gpt-4o-mini"
prompt = "tldr: {{.Transcript}}"
```

or edit the `prompt.txt` file in the config directory.
