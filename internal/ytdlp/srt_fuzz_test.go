package ytdlp

import (
	"math"
	"testing"
)

func FuzzParseSRT(f *testing.F) {
	for _, seed := range []string{
		"1\n00:00:00,000 --> 00:00:01,000\nHello\n",
		"1\r\n00:00:01,500 --> 00:00:02,000\r\n<b>Hello</b>\r\n",
		"1\n00:00:02,000 --> 00:00:01,000\nReversed\n",
		"not an SRT file",
		"\x00\xff",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, content string) {
		for _, segment := range parseSRT(content) {
			if math.IsNaN(segment.Start) || math.IsInf(segment.Start, 0) || math.IsNaN(segment.End) || math.IsInf(segment.End, 0) {
				t.Fatalf("parseSRT() returned non-finite timing: %+v", segment)
			}
			if segment.Start < 0 || segment.End < segment.Start {
				t.Fatalf("parseSRT() returned invalid timing: %+v", segment)
			}
		}
	})
}
