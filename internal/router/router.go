package router

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"stubr/internal/actions"
	"stubr/internal/config"
	"stubr/internal/matcher"
	"stubr/internal/responder"
)

type Router struct {
	cfg     *config.Config
	handler http.Handler
}

func New(cfg *config.Config) *Router {
	r := &Router{cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("/", r.handle)
	r.handler = mux
	return r
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt.handler.ServeHTTP(w, r)
}

func (rt *Router) handle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		path = "/"
	}
	method := r.Method

	if rt.cfg.Verbose {
		log.Printf("router: %s %s", method, path)
	}

	requestBody := responder.ReadBody(r)

	cfgRoute := rt.cfg.FindRoute(method, path)

	if rt.cfg.GlobalDelay > 0 {
		time.Sleep(time.Duration(rt.cfg.GlobalDelay) * time.Millisecond)
	}

	var filePath string
	var status int
	var respHeaders map[string]string
	var respBody string

	rw := responder.NewResponseWriter(w)

	if cfgRoute != nil {
		filePath = responder.FileFromConfig(cfgRoute)
		status = responder.StatusFromConfig(cfgRoute)
		respHeaders = mergeHeaders(rt.cfg.Headers, responder.HeadersFromConfig(cfgRoute))

		if cfgRoute.Response.Delay > 0 {
			time.Sleep(time.Duration(cfgRoute.Response.Delay) * time.Millisecond)
		}

		if filePath != "" {
			handler := responder.Respond(filePath, status, respHeaders)
			handler(rw, r)
		} else {
			handler := responder.RespondBytes([]byte{}, status, respHeaders)
			handler(rw, r)
		}
		respBody = responder.CopyResponseBody(rw)

		if len(cfgRoute.Actions) > 0 {
			tmplCtx := actions.BuildTemplateContext(r, requestBody, rw.Status(), respHeaders, respBody)
			go actions.Run(context.Background(), cfgRoute.Actions, tmplCtx)
		}
		return
	}

	if rt.cfg.DisableConvention {
		rt.serve404(w, r)
		return
	}

	match, err := matcher.MatchPath(rt.cfg.StubsDir, method, path)
	if err != nil {
		log.Printf("router: match error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":  "failed to match route",
			"detail": err.Error(),
		})
		return
	}

	if match != nil {
		status = http.StatusOK
		respHeaders = rt.cfg.Headers

		handler := responder.Respond(match.FilePath, status, respHeaders)
		handler(rw, r)
		respBody = responder.CopyResponseBody(rw)
		return
	}

	rt.serve404(w, r)
}

func (rt *Router) serve404(w http.ResponseWriter, r *http.Request) {
	available := matcher.ListAvailablePaths(rt.cfg.StubsDir)
	for i := range rt.cfg.Routes {
		available = append(available, rt.cfg.Routes[i].Method+" "+rt.cfg.Routes[i].Path)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":     "no stub for " + r.Method + " " + r.URL.Path,
		"available": available,
	})
}

func mergeHeaders(global, route map[string]string) map[string]string {
	merged := make(map[string]string)
	for k, v := range global {
		merged[k] = v
	}
	for k, v := range route {
		merged[k] = v
	}
	return merged
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
