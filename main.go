package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/drone/envsubst/v2"
	kongcompletion "github.com/jotaen/kong-completion"
	"github.com/posener/complete"
	"sigs.k8s.io/kustomize/kyaml/kio"

	"github.com/tobiash/k8q/internal/serve"
	k8qdiff "github.com/tobiash/k8q/pkg/diff"
	"github.com/tobiash/k8q/pkg/engine"
)

// Globals holds injected streams and global flags.
type Globals struct {
	In      io.Reader `kong:"-"`
	Out     io.Writer `kong:"-"`
	NoColor bool      `name:"no-color" help:"Disable colored output."`
	Output  string    `short:"o" name:"output" enum:"yaml,json" default:"yaml" help:"Output format (yaml or json)."`
	Files   []string  `short:"f" name:"file" type:"path" help:"Read from file(s) instead of stdin."`
}

// resolveInput returns the effective input reader. If --file flags are set,
// it reads and concatenates the files with YAML document separators.
// Otherwise it returns g.In (stdin by default).
func (g *Globals) resolveInput() (io.Reader, error) {
	if len(g.Files) == 0 {
		return g.In, nil
	}
	var buf bytes.Buffer
	for i, path := range g.Files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, userError("reading %s: %v", path, err)
		}
		if i > 0 {
			buf.WriteString("\n---\n")
		}
		buf.Write(data)
	}
	return &buf, nil
}

// runOrJSON executes a filter and writes output as either YAML or JSON depending
// on the global --output flag.
func runOrJSON(g *Globals, f kio.Filter) error {
	in, err := g.resolveInput()
	if err != nil {
		return err
	}
	if g.Output == "json" {
		nodes, err := engine.ReadNodes(in)
		if err != nil {
			return err
		}
		nodes, err = f.Filter(nodes)
		if err != nil {
			return err
		}
		return engine.WriteJSONList(g.Out, nodes)
	}
	return runPipeline(g, in, f)
}

// GetCmd filters manifests by kind, name, namespace, api group and/or label selector.
type GetCmd struct {
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)." predictor:"kind"`
	Kind      string `help:"Filter by kind." predictor:"kind"`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector (e.g. app=web,env!=staging,tier in (frontend,backend))."`
}

// Run executes the get command.
func (cmd *GetCmd) Run(g *Globals) error {
	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	f, err := engine.GetFilter(engine.GetOptions{
		Resource:  cmd.Resource,
		Kind:      cmd.Kind,
		Name:      cmd.Name,
		Namespace: cmd.Namespace,
		Group:     cmd.Group,
		Selector:  sel,
	})
	if err != nil {
		return err
	}
	return runOrJSON(g, f)
}

// SubstCmd substitutes environment variables in the raw input stream.
type SubstCmd struct {
	EnvFile string `flag:"--env-file" required:"" help:"Path to .env file." type:"path"`
}

// Run executes the subst command.
func (cmd *SubstCmd) Run(g *Globals) error {
	in, err := g.resolveInput()
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(in)
	if err != nil {
		return err
	}

	envData, err := os.ReadFile(cmd.EnvFile)
	if err != nil {
		return err
	}

	envMap := engine.EnvMapFromBytes(envData)

	substituted, err := envsubst.Eval(string(raw), func(s string) string { return envMap[s] })
	if err != nil {
		return err
	}

	return runPipeline(g, strings.NewReader(substituted))
}

// LabelCmd injects a label into manifests matching certain criteria.
type LabelCmd struct {
	Label     string `arg:"" help:"Label to inject (key=value)."`
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)." predictor:"kind"`
	Kind      string `help:"Filter by kind." predictor:"kind"`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector (e.g. app=web,env!=staging,tier in (frontend,backend))."`
}

// Run executes the label command.
func (cmd *LabelCmd) Run(g *Globals) error {
	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	f, err := engine.LabelFilter(engine.LabelOptions{
		Label: cmd.Label,
		Match: engine.MatchOptions{
			Resource:  cmd.Resource,
			Kind:      cmd.Kind,
			Name:      cmd.Name,
			Namespace: cmd.Namespace,
			Group:     cmd.Group,
			Selector:  sel,
			Mode:      engine.AndMode,
		},
	})
	if err != nil {
		return err
	}
	return runOrJSON(g, f)
}

// AnnotateCmd injects an annotation into matching manifests.
type AnnotateCmd struct {
	Annotation string `arg:"" help:"Annotation to inject (key=value)."`
	Resource   string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)." predictor:"kind"`
	Kind       string `help:"Filter by kind." predictor:"kind"`
	Name       string `help:"Filter by name."`
	Namespace  string `short:"n" help:"Filter by namespace."`
	Group      string `short:"g" help:"Filter by API group."`
	Selector   string `short:"l" help:"Label selector."`
}

// Run executes the annotate command.
func (cmd *AnnotateCmd) Run(g *Globals) error {
	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	f, err := engine.AnnotateFilter(engine.AnnotateOptions{
		Annotation: cmd.Annotation,
		Match: engine.MatchOptions{
			Resource:  cmd.Resource,
			Kind:      cmd.Kind,
			Name:      cmd.Name,
			Namespace: cmd.Namespace,
			Group:     cmd.Group,
			Selector:  sel,
			Mode:      engine.AndMode,
		},
	})
	if err != nil {
		return err
	}
	return runOrJSON(g, f)
}

// SetImageCmd updates container images in matching manifests.
type SetImageCmd struct {
	Image     string `arg:"" help:"Image to set (container=image:tag)."`
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)." predictor:"kind"`
	Kind      string `help:"Filter by kind." predictor:"kind"`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector."`
}

// Run executes the set-image command.
func (cmd *SetImageCmd) Run(g *Globals) error {
	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	f, err := engine.SetImageFilter(engine.SetImageOptions{
		Image: cmd.Image,
		Match: engine.MatchOptions{
			Resource:  cmd.Resource,
			Kind:      cmd.Kind,
			Name:      cmd.Name,
			Namespace: cmd.Namespace,
			Group:     cmd.Group,
			Selector:  sel,
			Mode:      engine.AndMode,
		},
	})
	if err != nil {
		return err
	}
	return runOrJSON(g, f)
}

// PatchCmd merges a YAML patch into matching manifests.
type PatchCmd struct {
	Patch     string `arg:"" help:"YAML patch snippet to merge."`
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)." predictor:"kind"`
	Kind      string `help:"Filter by kind." predictor:"kind"`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector."`
}

// Run executes the patch command.
func (cmd *PatchCmd) Run(g *Globals) error {
	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	f, err := engine.PatchFilter(engine.PatchOptions{
		Patch: cmd.Patch,
		Match: engine.MatchOptions{
			Resource:  cmd.Resource,
			Kind:      cmd.Kind,
			Name:      cmd.Name,
			Namespace: cmd.Namespace,
			Group:     cmd.Group,
			Selector:  sel,
			Mode:      engine.AndMode,
		},
	})
	if err != nil {
		return err
	}
	return runOrJSON(g, f)
}

// RemoveCmd deletes a field from matching manifests.
type RemoveCmd struct {
	Field     string `arg:"" help:"Field path to remove (e.g. spec.clusterIP)."`
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)." predictor:"kind"`
	Kind      string `help:"Filter by kind." predictor:"kind"`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector."`
}

// Run executes the remove command.
func (cmd *RemoveCmd) Run(g *Globals) error {
	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	f := engine.RemoveFilter(engine.RemoveOptions{
		Field: cmd.Field,
		Match: engine.MatchOptions{
			Resource:  cmd.Resource,
			Kind:      cmd.Kind,
			Name:      cmd.Name,
			Namespace: cmd.Namespace,
			Group:     cmd.Group,
			Selector:  sel,
			Mode:      engine.AndMode,
		},
	})
	return runOrJSON(g, f)
}

// ScaleCmd updates spec.replicas for matching manifests.
type ScaleCmd struct {
	Replicas  string `arg:"" help:"Number of replicas."`
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)." predictor:"kind"`
	Kind      string `help:"Filter by kind." predictor:"kind"`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector."`
}

// Run executes the scale command.
func (cmd *ScaleCmd) Run(g *Globals) error {
	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	f := engine.ScaleFilter(engine.ScaleOptions{
		Replicas: cmd.Replicas,
		Match: engine.MatchOptions{
			Resource:  cmd.Resource,
			Kind:      cmd.Kind,
			Name:      cmd.Name,
			Namespace: cmd.Namespace,
			Group:     cmd.Group,
			Selector:  sel,
			Mode:      engine.AndMode,
		},
	})
	return runOrJSON(g, f)
}

// RenameCmd prefixes or suffixes metadata.name.
type RenameCmd struct {
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)." predictor:"kind"`
	Prefix    string `help:"Prefix to add to name."`
	Suffix    string `help:"Suffix to add to name."`
	Kind      string `help:"Filter by kind." predictor:"kind"`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector."`
}

// Run executes the rename command.
func (cmd *RenameCmd) Run(g *Globals) error {
	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	f := engine.RenameFilter(engine.RenameOptions{
		Prefix: cmd.Prefix,
		Suffix: cmd.Suffix,
		Match: engine.MatchOptions{
			Resource:  cmd.Resource,
			Kind:      cmd.Kind,
			Name:      cmd.Name,
			Namespace: cmd.Namespace,
			Group:     cmd.Group,
			Selector:  sel,
			Mode:      engine.AndMode,
		},
	})
	return runOrJSON(g, f)
}

// CountCmd counts matching manifests.
type CountCmd struct {
	Resource    string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)." predictor:"kind"`
	Kind        string `help:"Filter by kind." predictor:"kind"`
	Name        string `help:"Filter by name."`
	Namespace   string `short:"n" help:"Filter by namespace."`
	Group       string `short:"g" help:"Filter by API group."`
	Selector    string `short:"l" help:"Label selector."`
	GroupByKind bool   `name:"group-by-kind" help:"Group counts by resource kind."`
}

// Run executes the count command.
func (cmd *CountCmd) Run(g *Globals) error {
	in, err := g.resolveInput()
	if err != nil {
		return err
	}

	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	opts := engine.CountOptions{
		GroupByKind: cmd.GroupByKind,
		Match: engine.MatchOptions{
			Resource:  cmd.Resource,
			Kind:      cmd.Kind,
			Name:      cmd.Name,
			Namespace: cmd.Namespace,
			Group:     cmd.Group,
			Selector:  sel,
			Mode:      engine.AndMode,
		},
	}

	if g.Output == "json" {
		nodes, err := engine.ReadNodes(in)
		if err != nil {
			return err
		}
		result, err := engine.CountJSON(nodes, opts)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(g.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	return runPipeline(g, in, engine.CountFilter(opts))
}

// SumCmd sums CPU and Memory requests for matching manifests.
type SumCmd struct {
	Resource        string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)." predictor:"kind"`
	Kind            string `help:"Filter by kind." predictor:"kind"`
	Name            string `help:"Filter by name."`
	Namespace       string `short:"n" help:"Filter by namespace."`
	Group           string `short:"g" help:"Filter by API group."`
	Selector        string `short:"l" help:"Label selector."`
	RequireRequests bool   `name:"require-requests" help:"Error if any matching container is missing resource requests."`
	RequireLimits   bool   `name:"require-limits" help:"Error if any matching container is missing resource limits."`
	MaxCPURequests  string `name:"max-cpu-requests" help:"Error if total CPU requests exceed this value."`
	MaxMemRequests  string `name:"max-mem-requests" help:"Error if total memory requests exceed this value."`
	MaxCPULimits    string `name:"max-cpu-limits" help:"Error if total CPU limits exceed this value."`
	MaxMemLimits    string `name:"max-mem-limits" help:"Error if total memory limits exceed this value."`
}

// Run executes the sum command.
func (cmd *SumCmd) Run(g *Globals) error {
	in, err := g.resolveInput()
	if err != nil {
		return err
	}

	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	opts := engine.SumOptions{
		RequireRequests: cmd.RequireRequests,
		RequireLimits:   cmd.RequireLimits,
		MaxCPURequests:  cmd.MaxCPURequests,
		MaxMemRequests:  cmd.MaxMemRequests,
		MaxCPULimits:    cmd.MaxCPULimits,
		MaxMemLimits:    cmd.MaxMemLimits,
		Match: engine.MatchOptions{
			Resource:  cmd.Resource,
			Kind:      cmd.Kind,
			Name:      cmd.Name,
			Namespace: cmd.Namespace,
			Group:     cmd.Group,
			Selector:  sel,
			Mode:      engine.AndMode,
		},
	}

	if g.Output == "json" {
		nodes, err := engine.ReadNodes(in)
		if err != nil {
			return err
		}
		result, err := engine.SumJSON(nodes, opts)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(g.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	return runPipeline(g, in, engine.SumFilter(opts))
}

// DropCmd removes manifests matching kind, name, namespace, api group and/or label selector.
type DropCmd struct {
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)." predictor:"kind"`
	Kind      string `help:"Filter by kind." predictor:"kind"`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector -- matching manifests are dropped."`
}

// Run executes the drop command.
func (cmd *DropCmd) Run(g *Globals) error {
	in, err := g.resolveInput()
	if err != nil {
		return err
	}

	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	opts := engine.DropOptions{
		Resource:  cmd.Resource,
		Kind:      cmd.Kind,
		Name:      cmd.Name,
		Namespace: cmd.Namespace,
		Group:     cmd.Group,
		Selector:  sel,
	}
	if err := opts.Validate(); err != nil {
		return err
	}

	return runPipeline(g, in, engine.DropFilter(opts))
}

// SetNamespaceCmd overwrites metadata.namespace on matching manifests.
type SetNamespaceCmd struct {
	Namespace    string `arg:"" help:"Namespace to set."`
	Resource     string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)." predictor:"kind"`
	Kind         string `help:"Filter by kind." predictor:"kind"`
	Name         string `help:"Filter by name."`
	OldNamespace string `name:"old-namespace" short:"n" help:"Filter by current namespace."`
	Group        string `short:"g" help:"Filter by API group."`
	Selector     string `short:"l" help:"Label selector (e.g. app=web,env!=staging,tier in (frontend,backend))."`
}

// Run executes the set-namespace command.
func (cmd *SetNamespaceCmd) Run(g *Globals) error {
	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	f := engine.SetNamespaceFilter(engine.NamespaceOptions{
		Namespace: cmd.Namespace,
		Match: engine.MatchOptions{
			Resource:  cmd.Resource,
			Kind:      cmd.Kind,
			Name:      cmd.Name,
			Namespace: cmd.OldNamespace,
			Group:     cmd.Group,
			Selector:  sel,
			Mode:      engine.AndMode,
		},
	})
	return runOrJSON(g, f)
}

// ServeCmd starts a mock Kubernetes API server serving piped-in manifests.
type ServeCmd struct {
	Port    int      `name:"port" short:"p" help:"Port to bind (default: random ephemeral)." default:"0"`
	Command []string `arg:"" optional:"" help:"Command and args to exec. Use -- to separate from k8q flags."`
}

// Run starts the mock API server.
func (cmd *ServeCmd) Run(g *Globals) error {
	in, err := g.resolveInput()
	if err != nil {
		return err
	}
	return serve.Run(in, cmd.Port, cmd.Command)
}

// DiffExitCode is returned when differences are found.
const DiffExitCode = 1

// DiffCmd compares two sets of Kubernetes manifests.
type DiffCmd struct {
	Files   []string `arg:"" optional:"" help:"Files to compare (0-2 args). Use - for stdin."`
	Base    string   `help:"Base (before) file. Stdin is used as after."`
	Summary bool     `help:"Print a summary instead of a unified diff."`
}

func (cmd *DiffCmd) Run(g *Globals) error {
	beforeReader, afterReader, cleanup, err := cmd.resolveInputs(g)
	if err != nil {
		return err
	}
	defer cleanup()

	beforeNodes, err := engine.ReadNodes(beforeReader)
	if err != nil {
		return fmt.Errorf("reading before: %w", err)
	}
	afterNodes, err := engine.ReadNodes(afterReader)
	if err != nil {
		return fmt.Errorf("reading after: %w", err)
	}

	result, err := k8qdiff.DiffNodes(beforeNodes, afterNodes)
	if err != nil {
		return err
	}

	if g.Output == "json" {
		jsonResult, err := k8qdiff.DiffNodesJSON(beforeNodes, afterNodes)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(g.Out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(jsonResult); err != nil {
			return err
		}
	} else {
		if cmd.Summary {
			k8qdiff.FormatSummary(g.Out, result)
		} else {
			k8qdiff.FormatUnifiedDiff(g.Out, result)
		}
	}

	if result.HasChanges() {
		return DiffExitError(DiffExitCode)
	}
	return nil
}

func (cmd *DiffCmd) resolveInputs(g *Globals) (before, after io.Reader, cleanup func(), err error) {
	cleanup = func() {}

	after, err = g.resolveInput()
	if err != nil {
		return nil, nil, cleanup, err
	}

	if cmd.Base != "" {
		f, err := os.Open(cmd.Base)
		if err != nil {
			return nil, nil, cleanup, userError("opening base file: %v", err)
		}
		cleanup = func() { f.Close() }
		return f, after, cleanup, nil
	}

	switch len(cmd.Files) {
	case 2:
		b, e1 := openFileOrStdin(cmd.Files[0], os.Stdin)
		a, e2 := openFileOrStdin(cmd.Files[1], os.Stdin)
		if e1 != nil {
			return nil, nil, cleanup, userError("opening %s: %v", cmd.Files[0], e1)
		}
		if e2 != nil {
			b.Close()
			return nil, nil, cleanup, userError("opening %s: %v", cmd.Files[1], e2)
		}
		cleanup = func() { b.Close(); a.Close() }
		return b, a, cleanup, nil
	case 1:
		f, err := os.Open(cmd.Files[0])
		if err != nil {
			return nil, nil, cleanup, userError("opening file %s: %v", cmd.Files[0], err)
		}
		cleanup = func() { f.Close() }
		return f, after, cleanup, nil
	default:
		return nil, nil, cleanup, userError("provide two files, or use --base with stdin")
	}
}

func openFileOrStdin(path string, stdin *os.File) (*os.File, error) {
	if path == "-" {
		return stdin, nil
	}
	return os.Open(path)
}

// DiffExitError wraps an exit code for the diff command.
type DiffExitError int

func (e DiffExitError) Error() string { return "differences found" }

// ErrUserInput marks errors caused by invalid CLI arguments, missing files,
// or other user-correctable mistakes. These exit with code 2.
var ErrUserInput = errors.New("user input error")

// userError wraps an error as a user input error.
func userError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUserInput, fmt.Sprintf(format, args...))
}

// CLI is the top-level Kong CLI struct.
type CLI struct {
	Globals

	Get          GetCmd                    `cmd:"" help:"Filter the stream to keep only matching manifests."`
	Drop         DropCmd                   `cmd:"" help:"Filter the stream to remove matching manifests."`
	Subst        SubstCmd                  `cmd:"" help:"Substitute environment variables from an .env file."`
	Label        LabelCmd                  `cmd:"" help:"Inject a label into matching manifests."`
	Annotate     AnnotateCmd               `cmd:"" help:"Inject an annotation into matching manifests."`
	SetImage     SetImageCmd               `cmd:"" help:"Update container images in matching manifests."`
	Patch        PatchCmd                  `cmd:"" help:"Merge a YAML patch into matching manifests."`
	Remove       RemoveCmd                 `cmd:"" help:"Delete a field from matching manifests."`
	Scale        ScaleCmd                  `cmd:"" help:"Update spec.replicas for matching manifests."`
	Rename       RenameCmd                 `cmd:"" help:"Prefix or suffix metadata.name for matching manifests."`
	SetNamespace SetNamespaceCmd           `cmd:"" help:"Overwrite metadata.namespace for matching manifests."`
	Count        CountCmd                  `cmd:"" help:"Count matching manifests."`
	Sum          SumCmd                    `cmd:"" help:"Sum CPU and Memory requests for matching manifests."`
	Diff         DiffCmd                   `cmd:"" help:"Compare two sets of Kubernetes manifests."`
	Serve        ServeCmd                  `cmd:"" help:"Start a mock Kubernetes API server for piped-in manifests."`
	Describe     DescribeCmd               `cmd:"" help:"Print JSON description of CLI and exit."`
	Completion   kongcompletion.Completion `cmd:"" help:"Print shell completion script."`
}

// DescribeCmd prints a JSON description of the CLI for programmatic discovery.
type DescribeCmd struct{}

func (cmd *DescribeCmd) Run(g *Globals) error {
	return describeCLI(g.Out, "k8q", "A Unix-style pipe for filtering, mutating, and exploring Kubernetes YAML manifests.", version, &CLI{})
}

func diffExitCode(err error) (int, bool) {
	var e DiffExitError
	if errors.As(err, &e) {
		return int(e), true
	}
	return 0, false
}

func main() {
	cli := &CLI{}
	cli.In = os.Stdin
	cli.Out = os.Stdout
	parser := kong.Must(cli,
		kong.Name("k8q"),
		kong.Description("A Unix-style pipe for filtering, mutating, and exploring Kubernetes YAML manifests."),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
			Summary: true,
		}),
		kong.DefaultEnvars(""),
		kong.Bind(&cli.Globals),
	)

	// Register completion support.
	kongcompletion.Register(parser, kongcompletion.WithPredictor("kind", complete.PredictSet(engine.CommonKinds...)))

	ctx, err := parser.Parse(os.Args[1:])
	if err != nil {
		// Parse errors are always user input errors → exit 2.
		if cli.Output == "json" {
			writeJSONError(cli.Out, err)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(2)
	}

	err = ctx.Run()
	if err != nil {
		if code, ok := serve.ExitCode(err); ok {
			os.Exit(code)
		}
		if code, ok := diffExitCode(err); ok {
			os.Exit(code)
		}

		// User-correctable errors → exit 2.
		exitCode := 1
		if errors.Is(err, ErrUserInput) {
			exitCode = 2
		}

		if cli.Output == "json" {
			writeJSONError(cli.Out, err)
			os.Exit(exitCode)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitCode)
	}
}

// jsonErrorEnvelope is the structured error output used when --output json is set.
type jsonErrorEnvelope struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data"`
	Error  jsonError   `json:"error"`
}

type jsonError struct {
	Reason  string                 `json:"reason"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

func writeJSONError(out io.Writer, err error) {
	env := jsonErrorEnvelope{
		Status: "failure",
		Data:   nil,
		Error: jsonError{
			Reason:  "CommandFailed",
			Message: err.Error(),
		},
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	_ = enc.Encode(env)
}
