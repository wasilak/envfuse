package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const DefaultShutdownTimeout = 10 * time.Second

type Config struct {
	ProviderType    string         `json:"provider_type"`
	LocalFilePath   string         `json:"local_file_path"`
	SecretPaths     []string       `json:"secret_paths"`
	EnvMappings     []EnvMapping   `json:"env_mappings,omitempty"`
	Templates       []TemplateSpec `json:"templates,omitempty"`
	ShutdownTimeout string         `json:"shutdown_timeout,omitempty"`
}

// ParsedShutdownTimeout returns the configured shutdown timeout, or the default
// value if not set (D-01). Returns an error if the value is not a valid duration.
func (c Config) ParsedShutdownTimeout() (time.Duration, error) {
	if c.ShutdownTimeout == "" {
		return DefaultShutdownTimeout, nil
	}
	d, err := time.ParseDuration(c.ShutdownTimeout)
	if err != nil {
		return 0, fmt.Errorf("shutdown_timeout: invalid duration %q: %w", c.ShutdownTimeout, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("shutdown_timeout: must be a positive duration, got %q", c.ShutdownTimeout)
	}
	return d, nil
}

type EnvMapping struct {
	Path   string `json:"path"`
	Key    string `json:"key"`
	EnvVar string `json:"env_var"`
}

type TemplateSpec struct {
	Name         string   `json:"name"`
	Content      string   `json:"content,omitempty"`
	TemplatePath string   `json:"template_path,omitempty"`
	OutputPath   string   `json:"output_path"`
	RequiredPaths []string `json:"required_paths,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()

	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	if cfg.ProviderType == "" {
		return Config{}, fmt.Errorf("provider_type is required")
	}
	if cfg.ProviderType == "local" && cfg.LocalFilePath == "" {
		return Config{}, fmt.Errorf("local_file_path is required for local provider")
	}

	seenEnvVars := make(map[string]struct{}, len(cfg.EnvMappings))
	for i, mapping := range cfg.EnvMappings {
		if mapping.Path == "" {
			return Config{}, fmt.Errorf("env_mappings[%d].path is required", i)
		}
		if mapping.Key == "" {
			return Config{}, fmt.Errorf("env_mappings[%d].key is required", i)
		}
		if mapping.EnvVar == "" {
			return Config{}, fmt.Errorf("env_mappings[%d].env_var is required", i)
		}
		if _, ok := seenEnvVars[mapping.EnvVar]; ok {
			return Config{}, fmt.Errorf("env_mappings[%d].env_var is duplicated: %s", i, mapping.EnvVar)
		}
		seenEnvVars[mapping.EnvVar] = struct{}{}
	}

	if _, err := cfg.ParsedShutdownTimeout(); err != nil {
		return Config{}, err
	}

	seenOutputs := make(map[string]struct{}, len(cfg.Templates))
	for i, tpl := range cfg.Templates {
		if tpl.OutputPath == "" {
			return Config{}, fmt.Errorf("templates[%d].output_path is required", i)
		}
		hasInline := tpl.Content != ""
		hasFile := tpl.TemplatePath != ""
		if hasInline == hasFile {
			return Config{}, fmt.Errorf("templates[%d] must set exactly one of content or template_path", i)
		}
		if _, exists := seenOutputs[tpl.OutputPath]; exists {
			return Config{}, fmt.Errorf("templates[%d].output_path is duplicated: %s", i, tpl.OutputPath)
		}
		seenOutputs[tpl.OutputPath] = struct{}{}
	}

	return cfg, nil
}
