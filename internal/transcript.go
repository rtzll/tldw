package internal

import "github.com/rtzll/tldw/internal/tldw"

// Compatibility aliases keep existing callers stable while the application
// module moves behind the tldw package seam.
type TranscriptSource = tldw.TranscriptSource
type TranscriptRenderFormat = tldw.TranscriptRenderFormat
type TranscriptSegment = tldw.TranscriptSegment
type Transcript = tldw.Transcript

const (
	TranscriptSourceCaptions = tldw.TranscriptSourceCaptions
	TranscriptSourceWhisper  = tldw.TranscriptSourceWhisper

	TranscriptRenderFormatPlain      = tldw.TranscriptRenderFormatPlain
	TranscriptRenderFormatTimestamps = tldw.TranscriptRenderFormatTimestamps
)

var ErrTranscriptTimestampsUnavailable = tldw.ErrTranscriptTimestampsUnavailable
