package router

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"stubr/internal/actions"
	"stubr/internal/config"
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
		log.Printf("router: error loading directory configs: %v", err)
	}

	return r
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt.mux.ServeHTTP(w, r)
}

func (rt *Router) handle(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
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

	var respBody string
	rw := responder.NewResponseWriter(w)

	if cfgRoute != nil {
		rt.serveConfigRoute(rw, r, cfgRoute, requestBody)
		respBody = responder.CopyResponseBody(rw)
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
		status, respHeaders, finalFile, delay, allActions := rt.resolveDirResponse(match, r)
		respHeaders = mergeHeaders(rt.cfg.Headers, respHeaders)

		if delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}

		handler := responder.Respond(finalFile, status, respHeaders)
		handler(rw, r)
		respBody = responder.CopyResponseBody(rw)

		if len(allActions) > 0 {
			tmplCtx := actions.BuildTemplateContext(r, requestBody, rw.Status(), respHeaders, respBody)
			go actions.Run(context.Background(), allActions, tmplCtx)
		}
		return
	}

	rt.serve404(w, r)
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

	qm := config.FindQueryMatch(dc, r.URL.Query())
	if qm != nil {
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
		tmplCtx := actions.BuildTemplateContext(r, requestBody, rw.Status(), respHeaders, responder.CopyResponseBody(rw))
		go actions.Run(context.Background(), cfgRoute.Actions, tmplCtx)
	}
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
