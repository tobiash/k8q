package main

import (
	"io"
	"os"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/drone/envsubst/v2"
	"github.com/tobiash/k8q/internal/engine"
)

// Globals holds injected streams and global flags.
type Globals struct {
	In      io.Reader `kong:"-"`
	Out     io.Writer `kong:"-"`
	NoColor bool      `name:"no-color" help:"Disable colored output."`
}

// GetCmd filters manifests by kind, name, namespace, api group and/or label selector.
type GetCmd struct {
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)."`
	Kind      string `help:"Filter by kind."`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector (e.g. app=web,env!=staging,tier in (frontend,backend))."`
}

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
	return runPipeline(g, f)
}

// SubstCmd substitutes environment variables in the raw input stream.
type SubstCmd struct {
	EnvFile string `flag:"--env-file" required:"" help:"Path to .env file." type:"path"`
}

func (cmd *SubstCmd) Run(g *Globals) error {
	raw, err := io.ReadAll(g.In)
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

	// Use the substituted string as pipeline input.
	origIn := g.In
	g.In = strings.NewReader(substituted)
	defer func() { g.In = origIn }()

	return runPipeline(g)
}

// LabelCmd injects a label into manifests matching certain criteria.
type LabelCmd struct {
	Label     string `arg:"" help:"Label to inject (key=value)."`
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)."`
	Kind      string `help:"Filter by kind."`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector (e.g. app=web,env!=staging,tier in (frontend,backend))."`
}

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
	return runPipeline(g, f)
}

// AnnotateCmd injects an annotation into matching manifests.
type AnnotateCmd struct {
	Annotation string `arg:"" help:"Annotation to inject (key=value)."`
	Resource   string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)."`
	Kind       string `help:"Filter by kind."`
	Name       string `help:"Filter by name."`
	Namespace  string `short:"n" help:"Filter by namespace."`
	Group      string `short:"g" help:"Filter by API group."`
	Selector   string `short:"l" help:"Label selector."`
}

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
	return runPipeline(g, f)
}

// SetImageCmd updates container images in matching manifests.
type SetImageCmd struct {
	Image     string `arg:"" help:"Image to set (container=image:tag)."`
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)."`
	Kind      string `help:"Filter by kind."`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector."`
}

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
	return runPipeline(g, f)
}

// PatchCmd merges a YAML patch into matching manifests.
type PatchCmd struct {
	Patch     string `arg:"" help:"YAML patch snippet to merge."`
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)."`
	Kind      string `help:"Filter by kind."`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector."`
}

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
	return runPipeline(g, f)
}

// RemoveCmd deletes a field from matching manifests.
type RemoveCmd struct {
	Field     string `arg:"" help:"Field path to remove (e.g. spec.clusterIP)."`
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)."`
	Kind      string `help:"Filter by kind."`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector."`
}

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
	return runPipeline(g, f)
}

// ScaleCmd updates spec.replicas for matching manifests.
type ScaleCmd struct {
	Replicas  string `arg:"" help:"Number of replicas."`
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)."`
	Kind      string `help:"Filter by kind."`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector."`
}

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
	return runPipeline(g, f)
}

// RenameCmd prefixes or suffixes metadata.name.
type RenameCmd struct {
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)."`
	Prefix    string `help:"Prefix to add to name."`
	Suffix    string `help:"Suffix to add to name."`
	Kind      string `help:"Filter by kind."`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector."`
}

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
	return runPipeline(g, f)
}

// CountCmd counts matching manifests.
type CountCmd struct {
	Resource    string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)."`
	Kind        string `help:"Filter by kind."`
	Name        string `help:"Filter by name."`
	Namespace   string `short:"n" help:"Filter by namespace."`
	Group       string `short:"g" help:"Filter by API group."`
	Selector    string `short:"l" help:"Label selector."`
	GroupByKind bool   `name:"group-by-kind" help:"Group counts by resource kind."`
}

func (cmd *CountCmd) Run(g *Globals) error {
	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	f := engine.CountFilter(engine.CountOptions{
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
	})
	return runPipeline(g, f)
}

// SumCmd sums CPU and Memory requests for matching manifests.
type SumCmd struct {
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)."`
	Kind      string `help:"Filter by kind."`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector."`
}

func (cmd *SumCmd) Run(g *Globals) error {
	sel, err := engine.ParseSelectorFlag(cmd.Selector)
	if err != nil {
		return err
	}

	f := engine.SumFilter(engine.SumOptions{
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
	return runPipeline(g, f)
}

// DropCmd removes manifests matching kind, name, namespace, api group and/or label selector.
type DropCmd struct {
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)."`
	Kind      string `help:"Filter by kind."`
	Name      string `help:"Filter by name."`
	Namespace string `short:"n" help:"Filter by namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector -- matching manifests are dropped."`
}

func (cmd *DropCmd) Run(g *Globals) error {
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

	return runPipeline(g, engine.DropFilter(opts))
}

// SetNamespaceCmd overwrites metadata.namespace on matching manifests.
type SetNamespaceCmd struct {
	Namespace string `arg:"" help:"Namespace to set."`
	Resource  string `arg:"" optional:"" help:"Resource filter (kind, kind/name, or api-group)."`
	Kind      string `help:"Filter by kind."`
	Name      string `help:"Filter by name."`
	OldNamespace string `name:"old-namespace" short:"n" help:"Filter by current namespace."`
	Group     string `short:"g" help:"Filter by API group."`
	Selector  string `short:"l" help:"Label selector (e.g. app=web,env!=staging,tier in (frontend,backend))."`
}

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
	return runPipeline(g, f)
}

// CLI is the top-level Kong CLI struct.
type CLI struct {
	Get          GetCmd          `cmd:"" help:"Filter the stream to keep only matching manifests."`
	Drop         DropCmd         `cmd:"" help:"Filter the stream to remove matching manifests."`
	Subst        SubstCmd        `cmd:"" help:"Substitute environment variables from an .env file."`
	Label        LabelCmd        `cmd:"" help:"Inject a label into matching manifests."`
	Annotate     AnnotateCmd     `cmd:"" help:"Inject an annotation into matching manifests."`
	SetImage     SetImageCmd     `cmd:"" help:"Update container images in matching manifests."`
	Patch        PatchCmd        `cmd:"" help:"Merge a YAML patch into matching manifests."`
	Remove       RemoveCmd       `cmd:"" help:"Delete a field from matching manifests."`
	Scale        ScaleCmd        `cmd:"" help:"Update spec.replicas for matching manifests."`
	Rename       RenameCmd       `cmd:"" help:"Prefix or suffix metadata.name for matching manifests."`
	SetNamespace SetNamespaceCmd `cmd:"" help:"Overwrite metadata.namespace for matching manifests."`
	Count        CountCmd        `cmd:"" help:"Count matching manifests."`
	Sum          SumCmd          `cmd:"" help:"Sum CPU and Memory requests for matching manifests."`
}

func main() {
	ctx := kong.Parse(&CLI{},
		kong.Name("k8q"),
		kong.Description("A Unix-style pipe for filtering, mutating, and exploring Kubernetes YAML manifests."),
		kong.Bind(&Globals{
			In:  os.Stdin,
			Out: os.Stdout,
		}),
	)

	ctx.FatalIfErrorf(ctx.Run())
}
