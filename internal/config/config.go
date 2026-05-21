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

	return cfg, nil
}
