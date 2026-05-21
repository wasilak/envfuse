package fileio

import (
	"fmt"
	"path/filepath"
	"strings"

	"envfuse/internal/config"
)

func ValidateTemplateTargets(specs []config.TemplateSpec) error {
	allow := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		clean := filepath.Clean(spec.OutputPath)
		if !filepath.IsAbs(clean) {
			return fmt.Errorf("output path must be absolute: %s", spec.OutputPath)
		}
		if strings.Contains(clean, "..") {
			return fmt.Errorf("output path contains traversal: %s", spec.OutputPath)
		}
		allow[clean] = struct{}{}
	}

	for _, spec := range specs {
		clean := filepath.Clean(spec.OutputPath)
		if _, ok := allow[clean]; !ok {
			return fmt.Errorf("output path is not declared: %s", spec.OutputPath)
		}
	}

	return nil
}
