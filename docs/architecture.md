# tldw architecture

`tldw` is a CLI and MCP server built around one transport-neutral application
module. Dependencies point inward: transports compose the application, while the
application knows only domain interfaces.

## Package map

```text
main.go
└── cmd/                    Cobra commands, terminal presentation, composition
    ├── build.go            Constructs production adapters and the engine
    ├── flags.go            Maps Cobra flags into runtime configuration
    └── mcp.go              Starts the MCP transport

internal/
├── tldw/                   Domain model and application workflows
├── store/                  Filesystem transcript/metadata adapter
├── ytdlp/                  Video adapter
│   ├── client.go           Construction, public interface, shared command policy
│   ├── metadata.go         Video metadata and caption-language discovery
│   ├── captions.go         Caption selection, download, and cache lookup
│   ├── srt.go              Deterministic subtitle parsing and normalization
│   ├── audio.go            Audio download and cache placement
│   └── playlist.go         Playlist decoding and video-reference validation
├── openai/                 OpenAI summaries, transcription and ffmpeg audio preparation
├── mcp/                    MCP tools and HTTP/stdio transports
├── telemetry/              Trace provider, local span recorder and OTLP export
├── process/                External command execution and error reporting
├── config.go               XDG configuration used by CLI composition
├── prompt.go               Filesystem-backed prompt template adapter
└── progress.go             Terminal summary spinner

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

1. A CLI command or MCP tool parses input into a validated `tldw.ContentRef`.
2. The transport calls `tldw.Engine`.
3. The engine checks the store through its persistence interface.
4. On a miss, the engine asks yt-dlp for metadata, captions, or audio.
5. Paid transcription and summaries go through the OpenAI adapter.
6. The engine returns domain output; CLI or MCP performs presentation.

The CLI, summaries, playlists, and MCP tools all call `Engine.Transcript`.
The caller chooses one acquisition intent. Both timestamped intents can only
fetch captions. A playlist that prompts for paid transcription starts each
item with free captions and applies approval to that item alone.

On a cache miss, `readCaptions` owns at most two caption attempts. A missing
track triggers a metadata refresh and a retry if captions remain available.
A download failure gets one delayed retry. The second result ends recovery.
Cancellation and rate limits end the request before paid fallback. The engine
alone decides whether the selected intent permits paid transcription. [Caption recovery](../internal/tldw/captions.go)

YouTube throttling is shared across requests. After a rate limit, one caller
probes for recovery when the delay expires. Its success can reopen access only
if no newer rate limit has arrived. Each transcript request also has a bounded
wait across its metadata and caption reads. [YouTube cooldown](../internal/tldw/youtube_cooldown.go)

Paid transcription prepares and size-checks every audio chunk before the first
upload. The request owns its temporary split directories and cleans them up on
every exit. The downloaded source audio stays in the cache. [Audio preparation](../internal/openai/client.go)

## Tracing

The CLI owns one OpenTelemetry provider per invocation. The MCP server owns one
for its lifetime. Application and adapter operations inherit the active span’s
provider through `context.Context`. Pass the derived context to child work,
including callbacks admitted by the YouTube cooldown. Each admitted fetch
records its own attempt. The cooldown keeps only the span context that
established its current deadline and links waiting requests back to that fetch.
A probe starts with a failed outcome until the fetch returns. A rate limit
updates the cooldown window. Only a successful probe from the current generation
can reopen access. Cancellation, failure, and panic release the probe and wake
waiting requests so another caller can try.
[Cooldown implementation](../internal/tldw/youtube_cooldown.go)

HTTP records arrival before routing or protocol validation and returns its
trace ID in `X-Tldw-Trace-Id`. Each MCP tool records its own outcome, including
tool errors carried by HTTP 200. W3C trace context in MCP `_meta` takes
precedence over HTTP headers. When the HTTP and MCP parents differ, the spans
link in both directions. Stdio callers can supply the same metadata and see
how long they waited for the serialized handler. The local HTTP listener is
the first observable boundary when an upstream client supplies no context.
[MCP instrumentation](../internal/mcp/tracing.go)

The local recorder writes a start and an end record for each span. End records
include elapsed time, outcome, attributes, events, and causal links when
present. OTLP batches completed spans and gets a bounded flush on shutdown.
Tracing records all spans when either sink is configured. Local files are
append-only and retention is managed by the operator.
[Provider and export](../internal/telemetry/runtime.go)

Use stable operation names and bounded failure categories. Video IDs, caption
languages, subprocess exit codes and HTTP statuses are useful diagnostic
fields. Keep raw URLs, arbitrary headers, user input, prompts, transcripts,
credentials, and subprocess output out of span names, attributes and events.
Use span links for dependencies between requests and parent spans for work
performed by one request. The concurrency regression test asserts that a second
video can be deferred without running yt-dlp and identifies the first video’s
fetch as the cause. [Concurrency regression test](../internal/ytdlp/tracing_test.go)

## Persistence

The filesystem store owns all on-disk formats:

- `<video-id>.captions.transcript.json` — captions with verified origin and optional timing
- `<video-id>.paid.transcript.json` — paid plain-text transcript
- `<video-id>.transcript.json` — earlier single-record format, read by recorded origin
- `<video-id>.txt` — plain-text compatibility copy of the most recently saved transcript
- `<video-id>.meta.json` — versioned metadata cache with first-seen time used by
  unique-video stats

Path validation is inside the store adapter. Audio files live under the XDG
cache directory and are managed by external adapters.

The engine checks the transcript cache after acquiring the per-video lock.
Concurrent CLI and MCP processes can reuse the completed result. Each file is
published atomically. Structured JSON is committed first and stays authoritative
if writing the plain-text copy fails. A separate metadata lock protects the
original first-seen time across concurrent updates. [Filesystem store](../internal/store/file.go)

## Verification

- `go test ./...` runs deterministic package and workflow tests.
- `go test -race ./...` checks concurrent paths under the race detector.
- `just fuzz` exercises reference and subtitle parsers with bounded fuzz runs.
- `just lint` runs the repository lint gate.
- `just smoke-transcription` builds the real binary, fetches the README video
  through the CLI, starts HTTP MCP, calls `get_youtube_transcript`, and requires
  the timestamped MCP result to match the CLI result exactly. It also checks
  captioned and uncaptioned X posts through both transports.

The smoke test is opt-in because it requires `yt-dlp` and network access. Pull
requests and pushes to `main` run module tidiness, tests, race tests, and lint
in CI; the external smoke remains a deliberate local/release gate.
