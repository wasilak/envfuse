package render

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"

	"envfuse/internal/config"
)

type Output struct {
	Path    string
	Content []byte
}

func RenderTemplates(snapshot map[string]map[string]any, specs []config.TemplateSpec) ([]Output, error) {
	outputs := make([]Output, 0, len(specs))
	for _, spec := range specs {
		src, err := loadTemplateSource(spec)
		if err != nil {
			return nil, err
		}

		tpl, err := template.New(spec.Name).Option("missingkey=error").Parse(src)
		if err != nil {
			return nil, fmt.Errorf("template parse failed (%s): %w", spec.OutputPath, err)
		}

		var buf bytes.Buffer
		if err := tpl.Execute(&buf, snapshot); err != nil {
			return nil, fmt.Errorf("template render failed (%s): %w", spec.OutputPath, err)
		}
		if strings.Contains(buf.String(), "<no value>") {
			return nil, fmt.Errorf("template render failed (%s): missing key", spec.OutputPath)
		}

		outputs = append(outputs, Output{Path: spec.OutputPath, Content: buf.Bytes()})
	}

	return outputs, nil
}

func loadTemplateSource(spec config.TemplateSpec) (string, error) {
	if spec.Content != "" {
		return spec.Content, nil
	}

	b, err := os.ReadFile(spec.TemplatePath)
	if err != nil {
		return "", fmt.Errorf("read template_path %s: %w", spec.TemplatePath, err)
	}

	return string(b), nil
}
