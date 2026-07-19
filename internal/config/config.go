package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ActionType string

const (
	ActionCommand ActionType = "command"
	ActionWebhook ActionType = "webhook"
	ActionScript  ActionType = "script"
)

// --- Root config ---

type Config struct {
	Port              int               `yaml:"port"`
	Host              string            `yaml:"host"`
	StubsDir          string            `yaml:"stubs_dir"`
	GlobalDelay       int               `yaml:"global_delay"`
	Verbose           bool              `yaml:"verbose"`
	DisableConvention bool              `yaml:"disable_convention"`
	Headers           map[string]string `yaml:"headers"`
	Routes            []Route           `yaml:"routes"`
}

type Route struct {
	Path     string         `yaml:"path"`
	Method   string         `yaml:"method"`
	Response ResponseConfig `yaml:"response"`
	Actions  []Action       `yaml:"actions"`
}

type ResponseConfig struct {
	File    string            `yaml:"file"`
	Status  int               `yaml:"status"`
	Headers map[string]string `yaml:"headers"`
	Delay   int               `yaml:"delay"`
}

// --- Directory-level config (_stubr.yaml) ---

type DirConfig struct {
	Status     int                   `yaml:"status"`
	Delay      int                   `yaml:"delay"`
	Headers    map[string]string     `yaml:"headers"`
	File       string                `yaml:"file"`
	Actions    []Action              `yaml:"actions"`
	Methods    map[string]*DirConfig `yaml:"methods"`
	QueryMatch []QueryMatch          `yaml:"query_match"`
}

type QueryMatch struct {
	Params  map[string]string `yaml:"params"`
	Status  int               `yaml:"status"`
	Delay   int               `yaml:"delay"`
	Headers map[string]string `yaml:"headers"`
	File    string            `yaml:"file"`
	Actions []Action          `yaml:"actions"`
}

// --- Template context types ---

type TemplateContext struct {
	Request   RequestContext
	Response  ResponseContext
	Env       map[string]string
	Timestamp string
}

type RequestContext struct {
	Method  string
	Path    string
	Headers map[string]string
	Query   map[string]string
	Body    string
}

type ResponseContext struct {
	Status  int
	Headers map[string]string
	Body    string
}

// --- Action types ---

type Action struct {
	Type    ActionType        `yaml:"type"`
	Command string            `yaml:"command"`
	Timeout string            `yaml:"timeout"`
	Env     map[string]string `yaml:"env"`
	URL     string            `yaml:"url"`
	Method  string            `yaml:"method"`
	Headers map[string]string `yaml:"headers"`
	Body    string            `yaml:"body"`
	Retry   int               `yaml:"retry"`
	Inline  string            `yaml:"inline"`
}

// --- Config loading ---

func DefaultConfig() *Config {
	return &Config{
		Port:     8080,
		Host:     "0.0.0.0",
		StubsDir: "./stubs",
	}
}

func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	for i, route := range c.Routes {
		if route.Method == "" {
			return fmt.Errorf("invalid config: routes[%d]: method is required", i)
		}
		if route.Path == "" {
			return fmt.Errorf("invalid config: routes[%d]: path is required", i)
		}
		if !strings.HasPrefix(route.Path, "/") {
			return fmt.Errorf("invalid config: routes[%d]: path must start with /", i)
		}
		route.Method = strings.ToUpper(route.Method)

		for j, action := range route.Actions {
			if action.Timeout != "" {
				if _, err := time.ParseDuration(action.Timeout); err != nil {
					return fmt.Errorf("invalid config: routes[%d].actions[%d]: invalid timeout %q: %w", i, j, action.Timeout, err)
				}
			}
			switch action.Type {
			case ActionCommand:
				if action.Command == "" {
					return fmt.Errorf("invalid config: routes[%d].actions[%d]: command action requires command", i, j)
				}
			case ActionWebhook:
				if action.URL == "" {
					return fmt.Errorf("invalid config: routes[%d].actions[%d]: webhook action requires url", i, j)
				}
			case ActionScript:
				if action.Inline == "" {
					return fmt.Errorf("invalid config: routes[%d].actions[%d]: script action requires inline", i, j)
				}
			default:
				return fmt.Errorf("invalid config: routes[%d].actions[%d]: unknown action type %q", i, j, action.Type)
			}
		}
	}
	return nil
}

func (c *Config) FindRoute(method, path string) *Route {
	for i := range c.Routes {
		r := &c.Routes[i]
		if strings.EqualFold(r.Method, method) && r.Path == path {
			return r
		}
	}
	return nil
}

func (c *Config) MergeCLI(port int, host, dir string) {
	if port > 0 {
		c.Port = port
	}
	if host != "" {
		c.Host = host
	}
	if dir != "" {
		c.StubsDir = dir
	}
}

// --- DirConfig loading & merging ---

func LoadDirConfig(dir string) (*DirConfig, error) {
	cfgPath := filepath.Join(dir, "_stubr.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var dc DirConfig
	if err := yaml.Unmarshal(data, &dc); err != nil {
		return nil, fmt.Errorf("invalid _stubr.yaml in %s: %w", dir, err)
	}
	return &dc, nil
}

func MergeDirConfigs(base, override *DirConfig) *DirConfig {
	if base == nil {
		return override.clone()
	}
	if override == nil {
		return base.clone()
	}

	merged := base.clone()

	if override.Status != 0 {
		merged.Status = override.Status
	}
	if override.Delay != 0 {
		merged.Delay = override.Delay
	}
	if override.File != "" {
		merged.File = override.File
	}
	if override.Headers != nil {
		if merged.Headers == nil {
			merged.Headers = make(map[string]string)
		}
		for k, v := range override.Headers {
			merged.Headers[k] = v
		}
	}
	if override.Actions != nil {
		merged.Actions = append(merged.Actions, override.Actions...)
	}
	if override.QueryMatch != nil {
		merged.QueryMatch = append(override.QueryMatch, merged.QueryMatch...)
	}
	if override.Methods != nil {
		if merged.Methods == nil {
			merged.Methods = make(map[string]*DirConfig)
		}
		for name, m := range override.Methods {
			if existing, ok := merged.Methods[name]; ok {
				merged.Methods[name] = MergeDirConfigs(existing, m)
			} else {
				merged.Methods[name] = m.clone()
			}
		}
	}

	return merged
}

func ResolveMethodConfig(dc *DirConfig, method string) *DirConfig {
	if dc == nil {
		return nil
	}
	method = strings.ToUpper(method)

	base := dc.clone()
	base.Methods = nil

	if methodOverride, ok := dc.Methods[method]; ok {
		return MergeDirConfigs(base, methodOverride)
	}
	return base
}

func FindQueryMatch(dc *DirConfig, query url.Values) *QueryMatch {
	if dc == nil || len(dc.QueryMatch) == 0 {
		return nil
	}

	for i := range dc.QueryMatch {
		qm := &dc.QueryMatch[i]
		if matchParams(qm.Params, query) {
			return qm
		}
	}
	return nil
}

func matchParams(expected map[string]string, actual url.Values) bool {
	for key, want := range expected {
		got := actual.Get(key)
		if got != want {
			return false
		}
	}
	return true
}

func (dc *DirConfig) clone() *DirConfig {
	if dc == nil {
		return nil
	}
	c := &DirConfig{
		Status: dc.Status,
		Delay:  dc.Delay,
		File:   dc.File,
	}
	if dc.Headers != nil {
		c.Headers = make(map[string]string, len(dc.Headers))
		for k, v := range dc.Headers {
			c.Headers[k] = v
		}
	}
	if dc.Actions != nil {
		c.Actions = make([]Action, len(dc.Actions))
		copy(c.Actions, dc.Actions)
	}
	if dc.QueryMatch != nil {
		c.QueryMatch = make([]QueryMatch, len(dc.QueryMatch))
		for i := range dc.QueryMatch {
			qm := dc.QueryMatch[i]
			cloneQM := QueryMatch{
				Status: qm.Status,
				Delay:  qm.Delay,
				File:   qm.File,
			}
			if qm.Params != nil {
				cloneQM.Params = make(map[string]string, len(qm.Params))
				for k, v := range qm.Params {
					cloneQM.Params[k] = v
				}
			}
			if qm.Headers != nil {
				cloneQM.Headers = make(map[string]string, len(qm.Headers))
				for k, v := range qm.Headers {
					cloneQM.Headers[k] = v
				}
			}
			if qm.Actions != nil {
				cloneQM.Actions = make([]Action, len(qm.Actions))
				copy(cloneQM.Actions, qm.Actions)
			}
			c.QueryMatch[i] = cloneQM
		}
	}
	if dc.Methods != nil {
		c.Methods = make(map[string]*DirConfig, len(dc.Methods))
		for k, v := range dc.Methods {
			c.Methods[k] = v.clone()
		}
	}
	return c
}
