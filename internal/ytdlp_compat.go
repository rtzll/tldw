package internal

import ytdlpadapter "github.com/rtzll/tldw/internal/ytdlp"

type YouTube = ytdlpadapter.YouTube
type PlaylistInfo = ytdlpadapter.PlaylistInfo
type VideoMetadata = ytdlpadapter.VideoMetadata
type VideoChapter = ytdlpadapter.VideoChapter

var ErrDownloadFailed = ytdlpadapter.ErrDownloadFailed

func NewYouTube(transcriptsDir string, verbose, quiet bool) *YouTube {
	return ytdlpadapter.NewYouTube(transcriptsDir, verbose, quiet)
}

func NewYouTubeWithCache(transcriptsDir, cacheDir string, verbose, quiet bool) *YouTube {
	return ytdlpadapter.NewYouTubeWithCache(transcriptsDir, cacheDir, verbose, quiet)
}
