package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"

	"github.com/alecthomas/kong"
)

// cliDescription is the JSON schema emitted by --describe.
type cliDescription struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Version     string           `json:"version"`
	Commands    []commandDesc    `json:"commands"`
}

type commandDesc struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Idempotent  bool      `json:"idempotent"`
	SideEffects bool      `json:"sideEffects"`
	Args        []argDesc `json:"args,omitempty"`
	Flags       []flagDesc `json:"flags,omitempty"`
}

type argDesc struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

type flagDesc struct {
	Name        string `json:"name"`
	Short       string `json:"short,omitempty"`
	Type        string `json:"type,omitempty"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
}

// describeCLI walks the Kong CLI struct and returns a JSON description.
func describeCLI(parser *kong.Kong, cli *CLI) ([]byte, error) {
	desc := cliDescription{
		Name:        parser.Model.Name,
		Description: parser.Model.Help,
		Version:     version,
	}

	// Walk the CLI struct fields looking for commands (fields with cmd tag).
	t := reflect.TypeOf(*cli)
	v := reflect.ValueOf(*cli)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if _, hasCmd := field.Tag.Lookup("cmd"); !hasCmd {
			continue // skip non-command fields (Globals, etc.)
		}

		cmd := commandDesc{
			Name:        strings.ToLower(field.Name),
			Description: field.Tag.Get("help"),
		}

		// Heuristic: mutators have side effects, get/diff/count/sum do not.
		switch cmd.Name {
		case "get", "count", "sum", "diff", "serve", "completion":
			cmd.Idempotent = true
			cmd.SideEffects = false
		case "subst":
			cmd.Idempotent = true
			cmd.SideEffects = false // subst is a pure transform
		default:
			cmd.Idempotent = false
			cmd.SideEffects = true
		}

		// Walk command struct for args and flags.
		cmdType := field.Type
		if cmdType.Kind() == reflect.Ptr {
			cmdType = cmdType.Elem()
		}
		cmdVal := v.Field(i)
		if cmdVal.Kind() == reflect.Ptr {
			cmdVal = cmdVal.Elem()
		}

		for j := 0; j < cmdType.NumField(); j++ {
			cmdField := cmdType.Field(j)
			if cmdField.Tag.Get("kong") == "-" {
				continue
			}

			arg := cmdField.Tag.Get("arg")
			if arg != "" {
				argDesc := argDesc{
					Name:        strings.ToLower(cmdField.Name),
					Required:    arg != "optional",
					Description: cmdField.Tag.Get("help"),
				}
				cmd.Args = append(cmd.Args, argDesc)
				continue
			}

			flagName := cmdField.Tag.Get("name")
			if flagName == "" {
				flagName = strings.ToLower(cmdField.Name)
			}
			if flagName == "" {
				continue
			}

			f := flagDesc{
				Name:        flagName,
				Short:       cmdField.Tag.Get("short"),
				Description: cmdField.Tag.Get("help"),
				Type:        cmdField.Tag.Get("type"),
			}
			if def := cmdField.Tag.Get("default"); def != "" {
				f.Default = def
			}
			cmd.Flags = append(cmd.Flags, f)
		}

		desc.Commands = append(desc.Commands, cmd)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return nil, enc.Encode(desc)
}
