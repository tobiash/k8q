package main

import (
	"sigs.k8s.io/kustomize/kyaml/kio"

	"github.com/tobiash/k8q/internal/engine"
	"github.com/tobiash/k8q/internal/prompt"
)

// runPipeline executes the YAML pipeline with automatic field reordering
// and optional colorization. It appends ReorderFilter to the filter chain
// and wraps the output writer for ANSI colors when writing to a terminal.
func runPipeline(g *Globals, filters ...kio.Filter) error {
	// Always reorder fields for consistent output.
	allFilters := append(filters, engine.ReorderFilter())

	// Wrap output for colorization when appropriate.
	out := g.Out
	if cw := maybeColorWriter(g); cw != nil {
		out = cw
		defer func() { _ = cw.Close() }()
	}

	return engine.Pipeline(g.In, out, allFilters...)
}

// maybeColorWriter returns a ColorWriter if colorization should be active,
// or nil if output should pass through unmodified.
func maybeColorWriter(g *Globals) *prompt.ColorWriter {
	if g.NoColor {
		return nil
	}
	if prompt.NoColorEnv() {
		return nil
	}
	if !prompt.IsTerminal(g.Out) {
		return nil
	}
	return prompt.NewColorWriter(g.Out, true)
}
