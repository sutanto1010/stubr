package responder

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"

	"stubr/internal/config"
	"stubr/internal/contenttype"
	"stubr/internal/logging"
)

func Respond(filePath string, status int, headers map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if status == 0 {
			status = http.StatusOK
		}

		f, err := os.Open(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "stub file not found",
					"file":  filePath,
				})
			} else {
				writeJSON(w, http.StatusInternalServerError, map[string]string{
					"error":  "failed to read stub file",
					"detail": err.Error(),
				})
			}
			return
		}
		defer f.Close()

		if headers != nil {
			for k, v := range headers {
				w.Header().Set(k, v)
			}
		}

		if _, hasCT := w.Header()["Content-Type"]; !hasCT {
			w.Header().Set("Content-Type", contenttype.FromExtension(filePath))
		}

		w.WriteHeader(status)

		if _, err := io.Copy(w, f); err != nil {
			logging.Error("error writing response body", "error", err)
		}
	}
}

func RespondBytes(data []byte, status int, headers map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if status == 0 {
			status = http.StatusOK
		}

		if headers != nil {
			for k, v := range headers {
				w.Header().Set(k, v)
			}
		}

		if _, hasCT := w.Header()["Content-Type"]; !hasCT {
			w.Header().Set("Content-Type", contenttype.DetectContentType(data))
		}

		w.WriteHeader(status)
		w.Write(data)
	}
}

func StatusFromConfig(route *config.Route) int {
	if route != nil && route.Response.Status != 0 {
		return route.Response.Status
	}
	return http.StatusOK
}

func HeadersFromConfig(route *config.Route) map[string]string {
	if route == nil {
		return nil
	}
	return route.Response.Headers
}

func FileFromConfig(route *config.Route) string {
	if route == nil {
		return ""
	}
	return route.Response.File
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func ReadBody(r *http.Request) string {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return ""
	}
	return string(body)
}

func CopyResponseBody(w *ResponseWriter) string {
	if w.buf != nil {
		return w.buf.String()
	}
	return ""
}

type ResponseWriter struct {
	http.ResponseWriter
	buf    *bytes.Buffer
	status int
}

func (rw *ResponseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *ResponseWriter) Write(b []byte) (int, error) {
	if rw.buf == nil {
		rw.buf = &bytes.Buffer{}
	}
	rw.buf.Write(b)
	return rw.ResponseWriter.Write(b)
}

func NewResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (rw *ResponseWriter) Status() int {
	return rw.status
}
