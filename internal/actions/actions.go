package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"stubr/internal/config"
	"stubr/internal/logging"
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
					logging.Error("action panic", "type", a.Type, "panic", r)
				}
			}()

			actionCtx, cancel := context.WithTimeout(ctx, parseTimeout(a.Timeout))
			defer cancel()

			logging.Info("action start", "type", a.Type)
			actionStart := time.Now()

			switch a.Type {
			case config.ActionCommand:
				runCommand(actionCtx, a, tmplCtx)
			case config.ActionWebhook:
				runWebhook(actionCtx, a, tmplCtx)
			case config.ActionScript:
				runScript(actionCtx, a, tmplCtx)
			}

			logging.Info("action end", "type", a.Type, "duration", time.Since(actionStart).String())
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
		logging.Error("template parse error", "error", err)
		return s
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		logging.Error("template execute error", "error", err)
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

	logging.Info("command running", "command", cmdStr)
	if err := cmd.Run(); err != nil {
		logging.Error("command failed", "command", cmdStr, "error", err)
	} else {
		logging.Info("command succeeded", "command", cmdStr)
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
			logging.Info("webhook succeeded", "method", method, "url", url, "attempt", attempt+1)
			return
		}

		logging.Warn("webhook failed",
			"method", method,
			"url", url,
			"attempt", attempt+1,
			"max_retries", maxRetries+1,
			"error", err,
		)

		if attempt < maxRetries {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
		}
	}

	logging.Error("webhook exhausted retries", "method", method, "url", url, "retries", maxRetries+1)
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
		logging.Error("failed to create temp file", "error", err)
		return
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(script); err != nil {
		logging.Error("failed to write script", "error", err)
		f.Close()
		return
	}
	f.Close()

	if err := os.Chmod(f.Name(), 0700); err != nil {
		logging.Error("failed to chmod script", "file", f.Name(), "error", err)
		return
	}

	cmd := exec.CommandContext(ctx, "sh", f.Name())
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logging.Info("script running", "file", f.Name())
	if err := cmd.Run(); err != nil {
		logging.Error("script failed", "file", f.Name(), "error", err)
	} else {
		logging.Info("script succeeded", "file", f.Name())
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
