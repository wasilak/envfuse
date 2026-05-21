package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	ProviderType  string   `json:"provider_type"`
	LocalFilePath string   `json:"local_file_path"`
	SecretPaths   []string `json:"secret_paths"`
	EnvMappings   []EnvMapping `json:"env_mappings,omitempty"`
}

type EnvMapping struct {
	Path   string `json:"path"`
	Key    string `json:"key"`
	EnvVar string `json:"env_var"`
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

	return cfg, nil
}
