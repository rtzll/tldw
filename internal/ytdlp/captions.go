package ytdlp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rtzll/tldw/internal/tldw"
)

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

const (
	subLangsFlag            = "--sub-langs"
	englishFallbackSubLangs = "en.*,en"
)

var englishCaptionPreference = []string{"en-US", "en", "en-GB", "en-CA", "en-AU", "en-NZ", "en-orig"}

// setSubLangsArg updates the value that follows the --sub-langs flag in-place
func setSubLangsArg(args []string, value string) error {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == subLangsFlag {
			args[i+1] = value
			return nil
		}
	}
	return fmt.Errorf("sub-langs flag %q not found", subLangsFlag)
}

// buildSubLangs selects the primary --sub-langs value and an optional fallback.
// The fallback is the broad English wildcard, used when the primary language
// set does not yield any files.
func buildSubLangs(preferred []string, originalLang string) (primary string, fallback string) {
	if len(preferred) == 0 {
		return englishFallbackSubLangs, ""
	}

	langs := prioritizeCaptionLanguages(preferred, originalLang)

	if len(langs) == 0 {
		return englishFallbackSubLangs, ""
	}

	primary = strings.Join(langs, ",")

	if primary == englishFallbackSubLangs {
		return primary, ""
	}

	return primary, englishFallbackSubLangs
}

// prioritizeCaptionLanguages deduplicates languages, prefers English variants,
// and limits the count to avoid enormous --sub-langs lists that can trigger rate limits.
func prioritizeCaptionLanguages(preferred []string, originalLang string) []string {
	seen := make(map[string]struct{})
	var cleaned []string

	for _, lang := range preferred {
		lang = strings.TrimSpace(lang)
		if lang == "" || lang == "live_chat" {
			continue
		}
		if _, exists := seen[lang]; exists {
			continue
		}
		seen[lang] = struct{}{}
		cleaned = append(cleaned, lang)
	}

	// Put English variants first if present.
	var ordered []string
	for _, lang := range englishCaptionPreference {
		if _, ok := seen[lang]; ok {
			ordered = append(ordered, lang)
		}
	}

	// If we found any English variants, return just the first match to keep
	// requests minimal.
	if len(ordered) > 0 {
		return ordered[:1]
	}

	// If we have a declared original language and it exists in the captions,
	// prefer that single language.
	if originalLang != "" {
		if _, ok := seen[originalLang]; ok {
			return []string{originalLang}
		}
	}

	// Otherwise, take the first non-English language (if any) to keep the list
	// to a single entry.
	for _, lang := range cleaned {
		ordered = append(ordered, lang)
		break
	}

	return ordered
}

// downloadCaptions fetches subtitles using yt-dlp.
// preferredLangs allows us to target known caption languages (from metadata) instead of hardcoding English.
// originalLang is the video's declared language; when English captions are absent we prefer this.
func (yt *YouTube) downloadCaptions(ctx context.Context, ref tldw.YouTubeRef, preferredLangs []string, originalLang string) error {
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Downloading subtitles...\n")
	}

	// Create path in configured cache directory
	cacheDir := yt.cacheDir
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}

	// Set output path in cache directory
	outputPath := filepath.Join(cacheDir, "%(id)s")
	pattern := filepath.Join(cacheDir, fmt.Sprintf("%s*.srt", ref.ID()))

	primarySubLangs, fallbackSubLangs := buildSubLangs(preferredLangs, originalLang)

	args := []string{
		"--write-subs",      // Enable subtitle writing
		"--write-auto-subs", // Enable auto-generated subtitle writing
		"--sub-langs", primarySubLangs,
		"--convert-subs", "srt", // Convert subtitles to SRT format
		"--skip-download",       // Skip downloading the video
		"--sleep-interval", "2", // Sleep 2-5 seconds between requests to avoid rate limiting
		"--max-sleep-interval", "5",
		"--extractor-args", "youtube:player_client=web,android,-tv",
		"-o", outputPath, // Output to XDG cache directory
		ref.URL(),
	}

	// Run the command (and optional fallback)
	output, partialFiles, err := yt.runCaptionDownload(ctx, args, pattern)
	attemptedFallback := false
	if len(partialFiles) > 0 && yt.verbose && !yt.quiet {
		yt.log.Printf("Subtitles downloaded despite errors (%s): %v\n", primarySubLangs, partialFiles)
	}

	runFallback := func(message string) error {
		if yt.verbose && !yt.quiet {
			yt.log.Printf("%s\n", message)
		}
		if err := setSubLangsArg(args, fallbackSubLangs); err != nil {
			return fmt.Errorf("configuring fallback subtitle languages: %w", err)
		}
		attemptedFallback = true
		fallbackOutput, fallbackFiles, fallbackErr := yt.runCaptionDownload(ctx, args, pattern)
		if len(fallbackFiles) > 0 && yt.verbose && !yt.quiet {
			yt.log.Printf("Subtitles downloaded despite fallback errors (%s): %v\n", fallbackSubLangs, fallbackFiles)
		}
		if fallbackErr != nil {
			if yt.verbose {
				yt.log.Printf("Fallback subtitle download error: %v\n", fallbackErr)
				yt.log.Printf("Command output: %s\n", string(fallbackOutput))
			}
			return fmt.Errorf("%w: %v", tldw.ErrDownloadFailed, fallbackErr)
		}
		return nil
	}

	if err != nil {
		if yt.verbose {
			yt.log.Printf("Subtitle download error (%s): %v\n", primarySubLangs, err)
			yt.log.Printf("Command output: %s\n", string(output))
		}

		// Check if this was a rate limit error - if so, don't retry with more variants
		if strings.Contains(string(output), "429") || strings.Contains(string(output), "Too Many Requests") {
			return fmt.Errorf("%w: rate limited", tldw.ErrDownloadFailed)
		}

		// Retry with a broader English wildcard when available
		if fallbackSubLangs != "" {
			if err := runFallback(fmt.Sprintf("Trying fallback subtitle languages: %s", fallbackSubLangs)); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("%w: %v", tldw.ErrDownloadFailed, err)
		}
	}

	if yt.verbose && !yt.quiet {
		yt.log.Printf("Subtitle download completed\n")
	}

	// Check for the downloaded subtitle files
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		// If no files were found and we haven't tried the fallback yet, give it one more attempt
		if len(files) == 0 && fallbackSubLangs != "" && !attemptedFallback {
			message := fmt.Sprintf("No subtitles found for %s, retrying with: %s", primarySubLangs, fallbackSubLangs)
			if err := runFallback(message); err != nil {
				return err
			}
			files, err = filepath.Glob(pattern)
		}

		if err != nil || len(files) == 0 {
			if yt.verbose {
				yt.log.Printf("No subtitle files found after download\n")
				yt.log.Printf("Searched for pattern: %s\n", pattern)
			}
			return fmt.Errorf("no subtitle files found after download")
		}
	}

	if yt.verbose && !yt.quiet {
		yt.log.Printf("Found %d subtitle file(s): %v\n", len(files), files)
	}

	return nil
}

func (yt *YouTube) runCaptionDownload(ctx context.Context, args []string, pattern string) ([]byte, []string, error) {
	output, err := yt.executor.Run(ctx, "yt-dlp", args...)
	if err == nil {
		return output, nil, nil
	}

	files, globErr := filepath.Glob(pattern)
	if globErr == nil && len(files) > 0 {
		return output, files, nil
	}
	return output, nil, err
}

func (yt *YouTube) fetchStructuredTranscript(ctx context.Context, ref tldw.YouTubeRef, subLangs []string, originalLang string) (*tldw.Transcript, error) {
	if yt.verbose && !yt.quiet {
		yt.log.Printf("Looking for existing transcript for video ID: %s\n", ref.ID())
	}

	// Look for an existing transcript first
	transcriptPath, err := yt.findExistingTranscript(ref.ID())
	if err != nil {
		return nil, fmt.Errorf("error searching for existing transcript: %w", err)
	}

	if transcriptPath != "" {
		if yt.verbose && !yt.quiet {
			yt.log.Printf("Found existing transcript: %s\n", transcriptPath)
		}
		// Process the existing transcript
		return yt.processSrtTranscript(transcriptPath)
	}

	if yt.verbose && !yt.quiet {
		yt.log.Printf("No existing transcript found, attempting to download...\n")
	}

	// No existing transcript found, try to download one
	err = yt.downloadCaptions(ctx, ref, subLangs, originalLang)
	if err != nil {
		// Preserve the error type for retry logic
		return nil, err
	}

	// Look for the downloaded transcript
	transcriptPath, err = yt.findExistingTranscript(ref.ID())
	if err != nil || transcriptPath == "" {
		if yt.verbose {
			yt.log.Printf("Could not find downloaded transcript: %v\n", err)
		}
		return nil, fmt.Errorf("downloaded transcript not found")
	}

	if yt.verbose && !yt.quiet {
		yt.log.Printf("Successfully downloaded transcript: %s\n", transcriptPath)
	}

	return yt.processSrtTranscript(transcriptPath)
}

// findExistingTranscript locates a previously downloaded transcript
func (yt *YouTube) findExistingTranscript(videoID string) (string, error) {
	// Look in configured cache directory
	cacheDir := yt.cacheDir
	if fileExists(cacheDir) {
		cacheFiles, err := os.ReadDir(cacheDir)
		if err == nil {
			for _, entry := range cacheFiles {
				name := entry.Name()
				if strings.HasPrefix(name, videoID) && strings.HasSuffix(name, ".srt") {
					return filepath.Join(cacheDir, name), nil
				}
			}
		}
	}

	// Look in transcripts directory for already processed transcripts
	if fileExists(yt.transcriptsDir) {
		transcriptFiles, err := os.ReadDir(yt.transcriptsDir)
		if err == nil {
			for _, entry := range transcriptFiles {
				name := entry.Name()
				if strings.HasPrefix(name, videoID) && strings.HasSuffix(name, ".srt") {
					return filepath.Join(yt.transcriptsDir, name), nil
				}
			}
		}
	}

	return "", nil
}

// extractCaptionLanguages returns a sorted, de-duplicated list of caption languages.
func extractCaptionLanguages(subtitles, autoCaptions map[string]any) []string {
	langs := make(map[string]struct{})

	for lang := range subtitles {
		if lang == "live_chat" {
			continue
		}
		langs[lang] = struct{}{}
	}

	for lang := range autoCaptions {
		if lang == "live_chat" {
			continue
		}
		langs[lang] = struct{}{}
	}

	if len(langs) == 0 {
		return nil
	}

	result := make([]string, 0, len(langs))
	for lang := range langs {
		result = append(result, lang)
	}
	sort.Strings(result)
	return result
}
