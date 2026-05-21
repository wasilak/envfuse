package inject

import (
	"fmt"

	"envfuse/internal/config"
)

func ResolveEnvMappings(snapshot map[string]map[string]any, mappings []config.EnvMapping) (map[string]string, error) {
	envPayload := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		pathValues, ok := snapshot[mapping.Path]
		if !ok {
			return nil, fmt.Errorf("env mapping unresolved: path not found: %s", mapping.Path)
		}

		rawValue, ok := pathValues[mapping.Key]
		if !ok {
			return nil, fmt.Errorf("env mapping unresolved: key not found: %s:%s", mapping.Path, mapping.Key)
		}

		envPayload[mapping.EnvVar] = fmt.Sprint(rawValue)
	}

	return envPayload, nil
}
