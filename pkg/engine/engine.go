// Package engine provides the core YAML stream pipeline for k8q.
//
// It wraps kustomize/kyaml's kio package to provide a simple Pipeline
// function that reads YAML from stdin, applies filter functions, and writes
// the result to stdout. Comments, formatting, and field ordering are preserved
// by using kyaml's AST-based representation throughout.
package engine

import (
	"fmt"
	"io"

	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// Filter is a pipeline stage that receives all parsed nodes and returns the
// transformed subset. Each k8q command implements one Filter.
type Filter = kio.FilterFunc

// Pipeline reads a multi-document YAML stream from in, applies filters in
// order, and writes the resulting documents to out.
func Pipeline(in io.Reader, out io.Writer, filters ...kio.Filter) error {
	p := kio.Pipeline{
		Inputs: []kio.Reader{&kio.ByteReader{
			Reader:                in,
			OmitReaderAnnotations: true,
		}},
		Filters: filters,
		Outputs: []kio.Writer{&kio.ByteWriter{
			Writer: out,
		}},
		// ContinueOnEmptyResult so subst can produce output from
		// raw bytes even when the filter chain returns nothing.
		ContinueOnEmptyResult: true,
	}
	return p.Execute()
}

// ReadNodes parses a multi-document YAML stream into a slice of RNodes.
// Convenience wrapper for tests that don't need the full pipeline.
func ReadNodes(in io.Reader) ([]*yaml.RNode, error) {
	nodes, err := (&kio.ByteReader{
		Reader:                in,
		OmitReaderAnnotations: true,
	}).Read()
	if err != nil {
		return nil, fmt.Errorf("reading YAML stream: %w", err)
	}
	return nodes, nil
}

// WriteNodes writes a slice of RNodes to out as a multi-document YAML stream.
// Convenience wrapper for tests that don't need the full pipeline.
func WriteNodes(out io.Writer, nodes []*yaml.RNode) error {
	return (&kio.ByteWriter{Writer: out}).Write(nodes)
}
