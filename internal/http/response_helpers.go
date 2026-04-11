package httpx

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	stdhttp "net/http"
	"strings"
)

func writeJSON(w stdhttp.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeHTML(w stdhttp.ResponseWriter, status int, body string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func renderPHPInfoDocument(version string, output []byte) string {
	content := strings.TrimSpace(string(output))
	lowerContent := strings.ToLower(content)
	if strings.HasPrefix(lowerContent, "<!doctype html") || strings.HasPrefix(lowerContent, "<html") {
		return content
	}

	title := "PHP info"
	if strings.TrimSpace(version) != "" {
		title = fmt.Sprintf("PHP %s info", version)
	}

	var builder strings.Builder
	builder.WriteString("<!doctype html><html><head><meta charset=\"utf-8\">")
	builder.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	builder.WriteString("<title>")
	builder.WriteString(html.EscapeString(title))
	builder.WriteString("</title>")
	builder.WriteString("<style>")
	builder.WriteString("html{color-scheme:light;}body{margin:0;background:#ffffff;color:#111827;font:13px/1.5 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,Liberation Mono,Courier New,monospace;}pre{margin:0;padding:16px;white-space:pre-wrap;word-break:break-word;}")
	builder.WriteString("</style></head><body><pre>")
	builder.WriteString(html.EscapeString(content))
	builder.WriteString("</pre></body></html>")
	return builder.String()
}

func renderPHPInfoErrorDocument(message string) string {
	var builder strings.Builder
	builder.WriteString("<!doctype html><html><head><meta charset=\"utf-8\">")
	builder.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	builder.WriteString("<title>PHP info unavailable</title>")
	builder.WriteString("<style>")
	builder.WriteString("html{color-scheme:light;}body{margin:0;background:#f8fafc;color:#0f172a;font:14px/1.5 -apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;}main{padding:20px;}section{border:1px solid #d0d7de;background:#ffffff;padding:16px;}")
	builder.WriteString("</style></head><body><main><section>")
	builder.WriteString(html.EscapeString(strings.TrimSpace(message)))
	builder.WriteString("</section></main></body></html>")
	return builder.String()
}
