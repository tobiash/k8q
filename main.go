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
	SetNamespace SetNamespaceCmd `cmd:"" help:"Overwrite metadata.namespace for matching manifests."`
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
