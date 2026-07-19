package contenttype

import (
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

var extensionToMIME = map[string]string{
	".json":  "application/json",
	".xml":   "application/xml",
	".html":  "text/html",
	".htm":   "text/html",
	".txt":   "text/plain",
	".yaml":  "application/x-yaml",
	".yml":   "application/x-yaml",
	".csv":   "text/csv",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".svg":   "image/svg+xml",
	".pdf":   "application/pdf",
	".mp4":   "video/mp4",
	".mp3":   "audio/mpeg",
	".js":    "application/javascript",
	".css":   "text/css",
	".woff2": "font/woff2",
	".woff":  "font/woff",
	".ico":   "image/x-icon",
	".webp":  "image/webp",
	".webm":  "video/webm",
	".avif":  "image/avif",
}

func FromExtension(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if mimeType, ok := extensionToMIME[ext]; ok {
		return mimeType
	}

	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}

	return "application/octet-stream"
}

func DetectContentType(data []byte) string {
	ct := http.DetectContentType(data)
	if strings.HasPrefix(ct, "text/plain") && len(data) > 0 {
		if data[0] == '{' || data[0] == '[' {
			return "application/json"
		}
		if data[0] == '<' {
			return "text/html"
		}
	}
	return ct
}
