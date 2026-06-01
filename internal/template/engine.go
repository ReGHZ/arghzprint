package template

import (
	"fmt"

	"github.com/aymerick/raymond"
)

// Engine renders Handlebars templates with arbitrary data.
type Engine struct {
	store *Store
}

func NewEngine(store *Store) *Engine {
	return &Engine{store: store}
}

// Render fetches the template for jobType, renders it with data, and returns HTML.
// data is the raw payload map from the print job — template variables reference
// its keys directly (e.g. {{orderId}}, {{#each items}}).
func (e *Engine) Render(jobType string, data map[string]any) (string, error) {
	src, err := e.store.Get(jobType)
	if err != nil {
		return "", err
	}

	tpl, err := raymond.Parse(src)
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", jobType, err)
	}

	result, err := tpl.Exec(data)
	if err != nil {
		return "", fmt.Errorf("render template %s: %w", jobType, err)
	}

	return result, nil
}

// RenderRaw renders an arbitrary HTML template string without loading from store.
// Used for live preview in the Web UI.
func (e *Engine) RenderRaw(src string, data map[string]any) (string, error) {
	tpl, err := raymond.Parse(src)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	result, err := tpl.Exec(data)
	if err != nil {
		return "", fmt.Errorf("render: %w", err)
	}

	return result, nil
}
