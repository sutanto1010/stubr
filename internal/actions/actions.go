package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"stubr/internal/config"
)

const defaultTimeout = 30 * time.Second

func Run(ctx context.Context, actions []config.Action, tmplCtx *config.TemplateContext) {
	if len(actions) == 0 {
		return
	}

	for _, action := range actions {
		a := action
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("actions: panic in action %s: %v", a.Type, r)
				}
			}()

			actionCtx, cancel := context.WithTimeout(ctx, parseTimeout(a.Timeout))
			defer cancel()

			switch a.Type {
			case config.ActionCommand:
				runCommand(actionCtx, a, tmplCtx)
			case config.ActionWebhook:
				runWebhook(actionCtx, a, tmplCtx)
			case config.ActionScript:
				runScript(actionCtx, a, tmplCtx)
			}
		}()
	}
}

func parseTimeout(s string) time.Duration {
	if s == "" {
		return defaultTimeout
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultTimeout
	}
	return d
}

func renderTemplate(s string, ctx *config.TemplateContext) string {
	t, err := template.New("action").Parse(s)
	if err != nil {
		log.Printf("actions: template parse error: %v", err)
		return s
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		log.Printf("actions: template execute error: %v", err)
		return s
	}
	return buf.String()
}

func renderMap(m map[string]string, ctx *config.TemplateContext) map[string]string {
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = renderTemplate(v, ctx)
	}
	return result
}

func runCommand(ctx context.Context, action config.Action, tmplCtx *config.TemplateContext) {
	cmdStr := renderTemplate(action.Command, tmplCtx)
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Env = os.Environ()
	for k, v := range action.Env {
		cmd.Env = append(cmd.Env, k+"="+renderTemplate(v, tmplCtx))
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("actions: running command: %s", cmdStr)
	if err := cmd.Run(); err != nil {
		log.Printf("actions: command failed: %v", err)
	}
}

func runWebhook(ctx context.Context, action config.Action, tmplCtx *config.TemplateContext) {
	url := renderTemplate(action.URL, tmplCtx)
	method := action.Method
	if method == "" {
		method = "POST"
	}

	body := renderTemplate(action.Body, tmplCtx)
	headers := renderMap(action.Headers, tmplCtx)

	maxRetries := action.Retry
	if maxRetries < 0 {
		maxRetries = 0
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := doHTTP(ctx, method, url, body, headers)
		if err == nil {
			log.Printf("actions: webhook %s %s succeeded", method, url)
			return
		}

		log.Printf("actions: webhook %s %s attempt %d/%d failed: %v", method, url, attempt+1, maxRetries+1, err)

		if attempt < maxRetries {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
		}
	}
}

func doHTTP(ctx context.Context, method, url, body string, headers map[string]string) error {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: parseTimeout("")}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func runScript(ctx context.Context, action config.Action, tmplCtx *config.TemplateContext) {
	script := renderTemplate(action.Inline, tmplCtx)

	f, err := os.CreateTemp("", "stubr-script-*.sh")
	if err != nil {
		log.Printf("actions: failed to create temp file: %v", err)
		return
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(script); err != nil {
		log.Printf("actions: failed to write script: %v", err)
		f.Close()
		return
	}
	f.Close()

	if err := os.Chmod(f.Name(), 0700); err != nil {
		log.Printf("actions: failed to chmod script: %v", err)
		return
	}

	cmd := exec.CommandContext(ctx, "sh", f.Name())
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("actions: running script %s", f.Name())
	if err := cmd.Run(); err != nil {
		log.Printf("actions: script failed: %v", err)
	}
}

func BuildTemplateContext(r *http.Request, body string, status int, respHeaders map[string]string, respBody string) *config.TemplateContext {
	reqHeaders := make(map[string]string)
	for k, v := range r.Header {
		reqHeaders[k] = strings.Join(v, ", ")
	}

	query := make(map[string]string)
	for k, v := range r.URL.Query() {
		query[k] = strings.Join(v, ", ")
	}

	envMap := make(map[string]string)
	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	return &config.TemplateContext{
		Request: config.RequestContext{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: reqHeaders,
			Query:   query,
			Body:    body,
		},
		Response: config.ResponseContext{
			Status:  status,
			Headers: respHeaders,
			Body:    respBody,
		},
		Env:       envMap,
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

func PrettyJSON(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
