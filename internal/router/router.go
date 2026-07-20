package router

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"stubr/internal/actions"
	"stubr/internal/config"
	"stubr/internal/logging"
	"stubr/internal/matcher"
	"stubr/internal/responder"
)

type Router struct {
	cfg *config.Config
	mux *http.ServeMux
}

func New(cfg *config.Config) *Router {
	r := &Router{cfg: cfg}
	r.mux = http.NewServeMux()
	r.mux.HandleFunc("/", r.handle)

	if err := matcher.LoadDirConfigs(cfg.StubsDir); err != nil {
		logging.Error("failed to load directory configs", "error", err)
	}

	return r
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt.mux.ServeHTTP(w, r)
}

func (rt *Router) handle(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	method := r.Method

	rt.logRequestStart(method, path)

	requestBody := responder.ReadBody(r)

	cfgRoute := rt.cfg.FindRoute(method, path)

	if rt.cfg.GlobalDelay > 0 {
		time.Sleep(time.Duration(rt.cfg.GlobalDelay) * time.Millisecond)
	}

	var respBody string
	rw := responder.NewResponseWriter(w)

	if cfgRoute != nil {
		rt.serveConfigRoute(rw, r, cfgRoute, requestBody)
		respBody = responder.CopyResponseBody(rw)
		rt.logRequestEnd(method, path, rw.Status(), len(respBody), start)
		return
	}

	if rt.cfg.DisableConvention {
		rt.serve404(w, r, requestBody)
		rt.logRequestEnd(method, path, http.StatusNotFound, 0, start)
		return
	}

	match, err := matcher.MatchPath(rt.cfg.StubsDir, method, path)
	if err != nil {
		logging.Error("match error", "method", method, "path", path, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":  "failed to match route",
			"detail": err.Error(),
		})
		rt.logRequestEnd(method, path, http.StatusInternalServerError, 0, start)
		return
	}

	if match != nil {
		status, respHeaders, finalFile, delay, allActions := rt.resolveDirResponse(match, r)
		respHeaders = mergeHeaders(rt.cfg.Headers, respHeaders)

		if delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}

		handler := responder.Respond(finalFile, status, respHeaders)
		handler(rw, r)
		respBody = responder.CopyResponseBody(rw)

		if len(allActions) > 0 {
			logging.Info("dispatching actions", "method", method, "path", path, "count", len(allActions), "file", finalFile)
			tmplCtx := actions.BuildTemplateContext(r, requestBody, rw.Status(), respHeaders, respBody)
			go actions.Run(context.Background(), allActions, tmplCtx)
		}

		rt.logRequestEnd(method, path, rw.Status(), len(respBody), start)
		return
	}

	rt.serve404(w, r, requestBody)
	rt.logRequestEnd(method, path, http.StatusNotFound, 0, start)
}

func (rt *Router) logRequestStart(method, path string) {
	if rt.cfg.Verbose {
		logging.Debug("request start", "method", method, "path", path)
	} else {
		logging.Info("request start", "method", method, "path", path)
	}
}

func (rt *Router) logRequestEnd(method, path string, status, bodySize int, start time.Time) {
	duration := time.Since(start)
	logging.Info("request end",
		"method", method,
		"path", path,
		"status", status,
		"size", bodySize,
		"duration", duration.String(),
	)
}

func (rt *Router) resolveDirResponse(match *matcher.Match, r *http.Request) (status int, headers map[string]string, file string, delay int, allActions []config.Action) {
	status = http.StatusOK
	headers = make(map[string]string)
	file = match.FilePath
	delay = 0

	dc := match.DirConfig

	if dc != nil {
		status = dc.Status
		delay = dc.Delay
		if dc.Headers != nil {
			for k, v := range dc.Headers {
				headers[k] = v
			}
		}
		if dc.Actions != nil {
			allActions = append(allActions, dc.Actions...)
		}
		if dc.File != "" {
			file = resolveFile(match.MatchedDir, dc.File)
		}
	}

	qm := config.FindQueryMatch(dc, match.Params, r.URL.Query())
	if qm != nil {
		logging.Debug("query_match applied", "query", r.URL.RawQuery)
		if qm.Status != 0 {
			status = qm.Status
		}
		if qm.Delay > 0 {
			delay = qm.Delay
		}
		if qm.Headers != nil {
			for k, v := range qm.Headers {
				headers[k] = v
			}
		}
		if qm.Actions != nil {
			allActions = append(allActions, qm.Actions...)
		}
		if qm.File != "" {
			file = resolveFile(match.MatchedDir, qm.File)
		}
	} else if dc != nil && len(dc.QueryMatch) > 0 {
		params := make([]string, 0)
		for _, qm := range dc.QueryMatch {
			for k := range qm.Params {
				params = append(params, k)
			}
		}
		logging.Debug("query_match not matched", "query", r.URL.RawQuery, "expected_params", params)
	}

	if status == 0 {
		status = http.StatusOK
	}

	return
}

func resolveFile(matchedDir, relOrAbs string) string {
	if filepath.IsAbs(relOrAbs) {
		return relOrAbs
	}
	return filepath.Join(matchedDir, relOrAbs)
}

func (rt *Router) serveConfigRoute(rw *responder.ResponseWriter, r *http.Request, cfgRoute *config.Route, requestBody string) {
	filePath := responder.FileFromConfig(cfgRoute)
	status := responder.StatusFromConfig(cfgRoute)
	respHeaders := mergeHeaders(rt.cfg.Headers, responder.HeadersFromConfig(cfgRoute))

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

	if len(cfgRoute.Actions) > 0 {
		logging.Info("dispatching actions", "method", r.Method, "path", cfgRoute.Path, "count", len(cfgRoute.Actions), "source", "config_route")
		tmplCtx := actions.BuildTemplateContext(r, requestBody, rw.Status(), respHeaders, responder.CopyResponseBody(rw))
		go actions.Run(context.Background(), cfgRoute.Actions, tmplCtx)
	}
}

func (rt *Router) serve404(w http.ResponseWriter, r *http.Request, requestBody string) {
	available := matcher.ListAvailablePaths(rt.cfg.StubsDir)
	for i := range rt.cfg.Routes {
		available = append(available, rt.cfg.Routes[i].Method+" "+rt.cfg.Routes[i].Path)
	}

	queryParams := make(map[string]string)
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			queryParams[k] = v[0]
		}
	}

	fullPath := r.URL.Path
	if r.URL.RawQuery != "" {
		fullPath += "?" + r.URL.RawQuery
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":        "no stub for " + r.Method + " " + fullPath,
		"method":       r.Method,
		"path":         r.URL.Path,
		"query_params": queryParams,
		"body":         requestBody,
		"available":    available,
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
