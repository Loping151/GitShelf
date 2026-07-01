package render

import (
	"bytes"
	"strings"
)

// FileInfo describes a blob to be routed to a renderer.
type FileInfo struct {
	Path string
	Ext  string // lowercase, with leading dot (e.g. ".go")
	Size int64
}

// Detect builds a FileInfo from a path.
func Detect(path string, size int64) FileInfo {
	return FileInfo{Path: path, Ext: strings.ToLower(ext(path)), Size: size}
}

func ext(path string) string {
	base := path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		base = path[i+1:]
	}
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		return base[i:]
	}
	return ""
}

// IsBinary heuristically reports whether content is binary (contains a NUL in
// the first 8KB, like git's own check).
func IsBinary(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(b[:n], 0) >= 0
}

var imageExts = set(".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".avif", ".bmp", ".ico")
var audioExts = set(".mp3", ".m4a", ".ogg", ".oga", ".wav", ".flac", ".opus")
var videoExts = set(".mp4", ".webm", ".mov", ".mkv", ".m4v")
var csvExts = set(".csv", ".tsv")

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

// MIMEFor returns a best-effort content type for raw responses.
func MIMEFor(fi FileInfo, content []byte) string {
	switch fi.Ext {
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".bmp":
		return "image/bmp"
	case ".ico":
		return "image/x-icon"
	case ".pdf":
		return "application/pdf"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".opus":
		return "audio/opus"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".mkv":
		return "video/x-matroska"
	case ".json":
		return "application/json; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".html", ".htm":
		// never serve as text/html to avoid the raw endpoint executing scripts
		return "text/plain; charset=utf-8"
	}
	if content != nil && IsBinary(content) {
		return "application/octet-stream"
	}
	return "text/plain; charset=utf-8"
}
