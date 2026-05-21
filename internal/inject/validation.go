package inject

import (
	"fmt"

	"envfuse/internal/config"
)

func ValidateEnvMappings(mappings []config.EnvMapping) error {
	seen := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		if mapping.EnvVar == "" {
			return fmt.Errorf("env mapping invalid: env_var is required")
		}
		if _, ok := seen[mapping.EnvVar]; ok {
			return fmt.Errorf("env mapping invalid: duplicate env_var: %s", mapping.EnvVar)
		}
		seen[mapping.EnvVar] = struct{}{}
	}
	return nil
}
