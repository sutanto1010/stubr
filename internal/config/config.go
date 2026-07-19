package config

import (
	"fmt"
	"os"
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

type Action struct {
	Type    ActionType         `yaml:"type"`
	Command string             `yaml:"command"`
	Timeout string             `yaml:"timeout"`
	Env     map[string]string  `yaml:"env"`
	URL     string             `yaml:"url"`
	Method  string             `yaml:"method"`
	Headers map[string]string  `yaml:"headers"`
	Body    string             `yaml:"body"`
	Retry   int                `yaml:"retry"`
	Inline  string             `yaml:"inline"`
}

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
