package engine

import (
	"bytes"
	"strings"
	"testing"
)

const workloadWithResources = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: example
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: app
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              cpu: 250m
              memory: 128Mi
`

func TestSumFilter(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		opts       SumOptions
		wantErr    bool
		wantOutput string
	}{
		{
			name: "missing limits returns error",
			input: strings.ReplaceAll(workloadWithResources, `            limits:
              cpu: 250m
              memory: 128Mi
`, ""),
			opts:    SumOptions{RequireLimits: true},
			wantErr: true,
		},
		{
			name:       "required limits present",
			input:      workloadWithResources,
			opts:       SumOptions{RequireLimits: true},
			wantOutput: "Requests:\n  CPU: 200m\n  Memory: 128.00 Mi\nLimits:\n  CPU: 500m\n  Memory: 256.00 Mi\n",
		},
		{
			name:       "summary reaches pipeline writer",
			input:      workloadWithResources,
			wantOutput: "Requests:\n  CPU: 200m\n  Memory: 128.00 Mi\nLimits:\n  CPU: 500m\n  Memory: 256.00 Mi\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			err := Pipeline(strings.NewReader(tt.input), &output, SumFilter(tt.opts))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Pipeline() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got := output.String(); got != tt.wantOutput {
				t.Errorf("Pipeline() output = %q, want %q", got, tt.wantOutput)
			}
		})
	}
}

func TestSumJSON(t *testing.T) {
	tests := []struct {
		name string
		opts SumOptions
	}{
		{name: "totals"},
		{name: "required resources", opts: SumOptions{RequireRequests: true, RequireLimits: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := ReadNodes(strings.NewReader(workloadWithResources))
			if err != nil {
				t.Fatalf("ReadNodes() error = %v", err)
			}
			got, err := SumJSON(nodes, tt.opts)
			if err != nil {
				t.Fatalf("SumJSON() error = %v", err)
			}
			if got.Requests.CPU != "200m" || got.Requests.Memory != "128Mi" {
				t.Errorf("SumJSON() requests = %+v", got.Requests)
			}
			if got.Limits.CPU != "500m" || got.Limits.Memory != "256Mi" {
				t.Errorf("SumJSON() limits = %+v", got.Limits)
			}
		})
	}
}
