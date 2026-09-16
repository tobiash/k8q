package main

import (
	"encoding/json"
	"io"

	"github.com/alecthomas/kong"
)

// cliDescription is the JSON schema emitted by describe.
type cliDescription struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Version     string        `json:"version"`
	Commands    []commandDesc `json:"commands"`
}

type commandDesc struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Idempotent  bool       `json:"idempotent"`
	SideEffects bool       `json:"sideEffects"`
	Args        []argDesc  `json:"args,omitempty"`
	Flags       []flagDesc `json:"flags,omitempty"`
}

type argDesc struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

type flagDesc struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Short       string `json:"short,omitempty"`
	Type        string `json:"type,omitempty"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
}

// describeCLI walks the Kong CLI struct and emits a JSON description to w.
func describeCLI(w io.Writer, name, description, version string, cli *CLI) error {
	parser, err := kong.New(cli, kong.Name(name), kong.Description(description), kong.DefaultEnvars(""))
	if err != nil {
		return err
	}
	desc := cliDescription{
		Name:        name,
		Description: description,
		Version:     version,
	}

	for _, node := range parser.Model.Children {
		cmd := commandDesc{
			Name:        node.Name,
			Description: node.Help,
			SideEffects: true,
		}

		// These commands only read inputs and emit output; transforms do not
		// write back to files or a cluster. Serve can execute arbitrary code.
		// Unknown commands retain conservative defaults.
		switch cmd.Name {
		case "get", "drop", "label", "annotate", "set-image", "patch",
			"remove", "scale", "set-namespace", "count", "sum", "diff", "describe", "completion":
			cmd.Idempotent = true
			cmd.SideEffects = false
		case "rename", "subst":
			cmd.SideEffects = false
		}

		for _, arg := range node.Positional {
			cmd.Args = append(cmd.Args, argDesc{Name: arg.Name, Required: arg.Required, Description: arg.Help})
		}
		for _, group := range node.AllFlags(false) {
			for _, flag := range group {
				f := flagDesc{
					Name: flag.Name, Required: flag.Required, Description: flag.Help,
					Type: flag.Target.Type().String(), Default: flag.Default,
				}
				if flag.Tag.Type != "" {
					f.Type = flag.Tag.Type
				}
				if flag.Short != 0 {
					f.Short = string(flag.Short)
				}
				cmd.Flags = append(cmd.Flags, f)
			}
		}

		desc.Commands = append(desc.Commands, cmd)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(desc)
}
