package tldw_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/rtzll/tldw/internal/store"
	"github.com/rtzll/tldw/internal/tldw"
)

type blockingVideo struct {
	videoStub
	started chan string
	release chan struct{}
}

func (v *blockingVideo) DownloadAudio(ctx context.Context, ref tldw.YouTubeRef) (string, error) {
	v.started <- ref.ID()
	select {
	case <-v.release:
		return "audio", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

type countingAI struct {
	aiStub
	calls atomic.Int32
}

func (a *countingAI) Transcribe(context.Context, string) (string, error) {
	a.calls.Add(1)
	return "transcript", nil
}

func TestConcurrentTranscriptRequestsShareCacheAndAllowCancellation(t *testing.T) {
	dir := t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		video := &blockingVideo{started: make(chan string, 4), release: make(chan struct{})}
		ai := &countingAI{}
		engine, err := tldw.NewEngine(tldw.Config{}, tldw.Dependencies{Video: video, Store: store.NewFile(dir), AI: ai, Prompts: &promptStub{}})
		if err != nil {
			t.Fatal(err)
		}
		first, _ := tldw.ParseVideoRef(testVideoID)
		other, _ := tldw.ParseVideoRef("tAP1eZYEuKA")
		results := make(chan error, 3)
		run := func(ref tldw.YouTubeRef) {
			_, err := engine.Transcript(context.Background(), ref, tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyWhisperOnly})
			results <- err
		}
		go run(first)
		if got := <-video.started; got != first.ID() {
			t.Fatal(got)
		}
		go run(first)
		canceled := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			_, err := engine.Transcript(ctx, first, tldw.TranscriptRequest{Policy: tldw.TranscriptPolicyWhisperOnly})
			canceled <- err
		}()
		synctest.Wait()
		cancel()
		synctest.Wait()
		if err := <-canceled; !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter error=%v", err)
		}
		go run(other)
		if got := <-video.started; got != other.ID() {
			t.Fatalf("duplicate download started: %s", got)
		}
		synctest.Wait()
		if len(video.started) != 0 {
			t.Fatal("same video downloaded twice")
		}
		close(video.release)
		for range 3 {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
		if ai.calls.Load() != 2 {
			t.Fatalf("paid calls=%d, want one per video", ai.calls.Load())
		}
	})
}
