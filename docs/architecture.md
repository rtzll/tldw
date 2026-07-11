# tldw architecture

`tldw` is a CLI and MCP server built around one transport-neutral application
module. Dependencies point inward: transports compose the application, while
the application knows only domain interfaces.

## Package map

```text
main.go
└── cmd/                    Cobra commands, terminal presentation, composition
    ├── build.go            Constructs production adapters and the engine
    └── mcp.go              Starts the MCP transport

internal/
├── tldw/                   Domain model and application workflows
├── store/                  Filesystem transcript/metadata adapter
├── ytdlp/                  YouTube metadata, captions, playlists, and audio
├── openai/                 OpenAI/Whisper and ffmpeg audio preparation
├── mcp/                    MCP tools and HTTP/stdio transports
├── process/                Buffered and streaming command execution
├── config.go               XDG configuration used by CLI composition
├── prompt.go               Filesystem-backed prompt template adapter
├── progress.go             Terminal progress implementations
└── flags.go                Cobra flag handling

smoke/
└── transcription_test.go   Opt-in real CLI + HTTP MCP transcription check
```

## Dependency direction

```text
cmd ───────────────► tldw
 │                    ▲
 ├──► store ──────────┤
 ├──► ytdlp ──────────┤
 ├──► openai ─────────┤
 └──► mcp ────────────┘

ytdlp ──► process
openai ─► process
```

`internal/tldw` does not import transports or concrete adapters. It defines the
interfaces for video access, persistence, AI work, prompt construction, and
logging. [cmd/build.go](../cmd/build.go) supplies the concrete implementations.

## Primary workflow

1. A CLI command or MCP tool parses input into a validated `tldw.YouTubeRef`.
2. The transport calls `tldw.Engine`.
3. The engine checks the store through its persistence interface.
4. On a miss, the engine asks yt-dlp for metadata, captions, or audio.
5. Paid transcription and summaries go through the OpenAI adapter.
6. The engine returns domain output; CLI or MCP performs presentation.

The same `Engine.Transcript` workflow serves CLI transcription, summaries,
playlists, and MCP tools. This is the central behavior seam.

## Persistence

The filesystem store owns all on-disk formats:

- `<video-id>.transcript.json` — canonical timestamped transcript
- `<video-id>.txt` — plain-text compatibility cache
- `<video-id>.meta.json` — versioned metadata cache

Path validation is inside the store adapter. Audio files live under the XDG
cache directory and are managed by external adapters.

## Verification

- `go test ./...` runs deterministic package and workflow tests.
- `just lint` runs the repository lint gate.
- `just smoke-transcription` builds the real binary, fetches the README video
  through the CLI, starts HTTP MCP, calls `get_youtube_transcript`, and requires
  the timestamped MCP result to match the CLI result exactly.

The smoke test is opt-in because it requires `yt-dlp` and network access.
