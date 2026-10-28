package warp

import (
	"bytes"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

// RenderOptions holds the context for rendering a resource's instructions.
type RenderOptions struct {
	// Args are the positional arguments provided by the caller (typically for Commands).
	Args []string
	// Globals are WARP-managed contextual variables (e.g., project directories,
	// active environments) provided by the runtime.
	Globals map[string]any
}

// TemplateWorkspace represents the workspace view model for templates.
type TemplateWorkspace struct {
	Dir  string
	Path string
}

// TemplateProject represents the project view model for templates.
type TemplateProject struct {
	Name        string
	DisplayName string
	Dir         string
}

// TemplateContext represents the context view model for templates.
type TemplateContext struct {
	Instructions string
	Path         string
}

// TemplateAgent represents the agent view model for templates.
type TemplateAgent struct {
	Name        string
	Description string
	Dir         string
	Path        string
	Skills      []TemplateSkill
	Tools       []TemplateTool
	Commands    []TemplateCommand
	Subagents   []TemplateAgent
	Rules       []TemplateRule
}

// TemplateRule represents the rule view model for templates.
type TemplateRule struct {
	Name        string
	Description string
	Dir         string
	Path        string
	Priority    int
}

// TemplateSkill represents the skill view model for templates.
type TemplateSkill struct {
	Name         string
	Description  string
	UseWhen      string
	Keywords     []string
	ArgumentHint string
	Arguments    []Argument
	Dir          string
	Path         string
}

// TemplateCommand represents the command view model for templates.
type TemplateCommand struct {
	Name         string
	Description  string
	ArgumentHint string
	Arguments    []Argument
	Dir          string
	Path         string
	Tools        []TemplateTool
}

// TemplateDecision represents the decision view model for templates.
type TemplateDecision struct {
	Name         string
	Description  string
	ArgumentHint string
	Arguments    []Argument
	Dir          string
	Path         string
}

// TemplateTool represents the tool view model for templates.
type TemplateTool struct {
	Name        string
	Description string
}

// Render processes the resource's instructions as a template.
// It supports both standard Go text/template syntax ({{.Name}}) and a
// convenient shorthand syntax ($Name, $1).
func Render(res Resource, opts *RenderOptions) (string, error) {
	if opts == nil {
		opts = &RenderOptions{}
	}

	data, err := buildTemplateData(res, opts)
	if err != nil {
		return "", err
	}
	return renderTemplate(res.GetName(), getInstructions(res), data)
}

// renderTemplate is a shared helper that handles preprocessing, parsing, and execution.
func renderTemplate(name string, instructions string, data map[string]any) (string, error) {
	if instructions == "" {
		return "", nil
	}

	// Preprocess shorthand $Var to {{.Var}}
	tmplStr := preprocessShorthand(instructions)

	// Parse and execute the template
	t, err := template.New(name).Option("missingkey=zero").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// preprocessShorthand converts shell-style variables to Go template variables.
// Handles $var, ${var}, $1, and escapes $$.
func preprocessShorthand(input string) string {
	// 1. Escape literal $$
	s := strings.ReplaceAll(input, "$$", "%%ESCAPED_DOLLAR%%")

	// 2. Replace ${var} -> {{.var}} (or {{index . "var"}} if it's a number)
	reBraceNum := regexp.MustCompile(`\$\{([0-9]+)\}`)
	s = reBraceNum.ReplaceAllString(s, `{{index . "$1"}}`)

	reBrace := regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)
	s = reBrace.ReplaceAllString(s, "{{.$1}}")

	// 3. Replace $var or $1
	reVarNum := regexp.MustCompile(`\$([0-9]+)`)
	s = reVarNum.ReplaceAllString(s, `{{index . "$1"}}`)

	reVar := regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z0-9_]*)`)
	s = reVar.ReplaceAllString(s, "{{.$1}}")

	// 4. Restore literal $
	s = strings.ReplaceAll(s, "%%ESCAPED_DOLLAR%%", "$")

	return s
}

// buildTemplateData lifts fields from the resource to the top level.
func buildTemplateData(res Resource, opts *RenderOptions) (map[string]any, error) {
	data := make(map[string]any)

	// Initialize built-in shorthands to empty string to avoid <no value> outputs
	data["WorkspaceDir"] = ""
	data["WorkspacePath"] = ""
	data["ProjectDir"] = ""
	data["ContextPath"] = ""

	// Add globals first
	maps.Copy(data, opts.Globals)

	// Base Resource fields
	data["Name"] = res.GetName()
	meta := res.GetMetadata()
	if meta.DisplayName != "" {
		data["DisplayName"] = meta.DisplayName
	} else {
		data["DisplayName"] = res.GetName()
	}
	data["Description"] = meta.Description
	data["Labels"] = meta.Labels

	// Type-specific fields
	switch v := res.(type) {
	case *Context:
		if _, ok := data["Context"]; !ok {
			ctxPath := filepath.Join(v.Directory, ContextFileName)
			tc := TemplateContext{
				Path: ctxPath,
			}
			data["Context"] = tc
			data["ContextPath"] = tc.Path
		}
	case *WorkspaceDef:
		if _, ok := data["Workspace"]; !ok {
			tw := TemplateWorkspace{
				Dir:  v.Directory,
				Path: filepath.Join(v.Directory, WorkspaceFileName),
			}
			data["Workspace"] = tw
			data["WorkspaceDir"] = tw.Dir
			data["WorkspacePath"] = tw.Path
		}
	case *Agent:
		if _, ok := data["Agent"]; !ok {
			ta := TemplateAgent{
				Name:        v.Metadata.Name,
				Description: v.Metadata.Description,
				Dir:         v.Directory,
				Path:        filepath.Join(v.Directory, v.Metadata.Name+".md"),
			}
			data["Agent"] = ta
		}
		if _, ok := data["Models"]; !ok {
			data["Models"] = v.Spec.Models
		}
		if _, ok := data["Skills"]; !ok {
			if v.Spec.Skills != nil {
				data["Skills"] = *v.Spec.Skills
			} else {
				data["Skills"] = []string(nil)
			}
		}
		if _, ok := data["Commands"]; !ok {
			if v.Spec.Commands != nil {
				data["Commands"] = *v.Spec.Commands
			} else {
				data["Commands"] = []string(nil)
			}
		}
		if _, ok := data["Subagents"]; !ok {
			if v.Spec.Subagents != nil {
				data["Subagents"] = *v.Spec.Subagents
			} else {
				data["Subagents"] = []string(nil)
			}
		}
		data["Triggers"] = v.Spec.Triggers
		if v.Spec.Temperature != nil {
			data["Temperature"] = *v.Spec.Temperature
		} else {
			data["Temperature"] = 0.0
		}
	case *Skill:
		if _, ok := data["Skill"]; !ok {
			ts := TemplateSkill{
				Name:         v.Metadata.Name,
				Description:  v.Metadata.Description,
				UseWhen:      v.Spec.UseWhen,
				Keywords:     v.Spec.Keywords,
				ArgumentHint: v.Spec.ArgumentHint,
				Arguments:    v.Spec.Arguments,
				Dir:          v.Directory,
				Path:         filepath.Join(v.Directory, v.Metadata.Name+".md"),
			}
			data["Skill"] = ts
		}
		data["UseWhen"] = v.Spec.UseWhen
		data["Keywords"] = v.Spec.Keywords
		data["ArgumentHint"] = v.Spec.ArgumentHint
		data["Arguments"] = v.Spec.Arguments

		// Map arguments
		if len(opts.Args) > 0 {
			data["Args"] = opts.Args
			for i, arg := range opts.Args {
				data[fmt.Sprintf("%d", i+1)] = arg
			}
		}

		for i, arg := range v.Spec.Arguments {
			if i < len(opts.Args) && opts.Args[i] != "" {
				data[arg.Name] = opts.Args[i]
			} else if _, ok := data[arg.Name]; !ok || data[arg.Name] == "" {
				if arg.Default != "" {
					data[arg.Name] = arg.Default
				}
			}

			if arg.Required {
				val, ok := data[arg.Name]
				if !ok || val == "" || val == nil {
					return nil, fmt.Errorf("required argument %q is missing for skill %q", arg.Name, v.Metadata.Name)
				}
			}
		}
	case *Command:
		if _, ok := data["Command"]; !ok {
			tc := TemplateCommand{
				Name:         v.Metadata.Name,
				Description:  v.Metadata.Description,
				ArgumentHint: v.Spec.ArgumentHint,
				Arguments:    v.Spec.Arguments,
				Dir:          v.Directory,
				Path:         filepath.Join(v.Directory, v.Metadata.Name+".md"),
			}
			data["Command"] = tc
		}
		data["Models"] = v.Spec.Models
		data["Tools"] = v.Spec.Tools
		data["ArgumentHint"] = v.Spec.ArgumentHint
		data["Arguments"] = v.Spec.Arguments

		// Map arguments
		if len(opts.Args) > 0 {
			data["Args"] = opts.Args
			for i, arg := range opts.Args {
				data[fmt.Sprintf("%d", i+1)] = arg
			}
		}

		for i, arg := range v.Spec.Arguments {
			if i < len(opts.Args) && opts.Args[i] != "" {
				data[arg.Name] = opts.Args[i]
			} else if _, ok := data[arg.Name]; !ok || data[arg.Name] == "" {
				if arg.Default != "" {
					data[arg.Name] = arg.Default
				}
			}

			if arg.Required {
				val, ok := data[arg.Name]
				if !ok || val == "" || val == nil {
					return nil, fmt.Errorf("required argument %q is missing for command %q", arg.Name, v.Metadata.Name)
				}
			}
		}
	case *Decision:
		if _, ok := data["Decision"]; !ok {
			td := TemplateDecision{
				Name:         v.Metadata.Name,
				Description:  v.Metadata.Description,
				ArgumentHint: v.Spec.ArgumentHint,
				Arguments:    v.Spec.Arguments,
				Dir:          v.Directory,
				Path:         filepath.Join(v.Directory, v.Metadata.Name+".md"),
			}
			data["Decision"] = td
		}
		data["Models"] = v.Spec.Models
		data["Questions"] = v.Spec.Questions
		data["ArgumentHint"] = v.Spec.ArgumentHint
		data["Arguments"] = v.Spec.Arguments

		// Map arguments
		if len(opts.Args) > 0 {
			data["Args"] = opts.Args
			for i, arg := range opts.Args {
				data[fmt.Sprintf("%d", i+1)] = arg
			}
		}

		for i, arg := range v.Spec.Arguments {
			if i < len(opts.Args) && opts.Args[i] != "" {
				data[arg.Name] = opts.Args[i]
			} else if _, ok := data[arg.Name]; !ok || data[arg.Name] == "" {
				if arg.Default != "" {
					data[arg.Name] = arg.Default
				}
			}

			if arg.Required {
				val, ok := data[arg.Name]
				if !ok || val == "" || val == nil {
					return nil, fmt.Errorf("required argument %q is missing for decision %q", arg.Name, v.Metadata.Name)
				}
			}
		}
	case *Rule:
		if _, ok := data["Rule"]; !ok {
			tr := TemplateRule{
				Name:        v.Metadata.Name,
				Description: v.Metadata.Description,
				Dir:         v.Directory,
				Path:        filepath.Join(v.Directory, v.Metadata.Name+".md"),
				Priority:    v.Spec.Priority,
			}
			data["Rule"] = tr
			data["RuleDir"] = tr.Dir
			data["RulePath"] = tr.Path
		}
	}

	return data, nil
}

func getInstructions(res Resource) string {
	switch v := res.(type) {
	case *Context:
		return v.Spec.Instructions
	case *WorkspaceDef:
		return v.Spec.Instructions
	case *Agent:
		return v.Spec.Instructions
	case *Skill:
		return v.Spec.Instructions
	case *Command:
		return v.Spec.Instructions
	case *Decision:
		return v.Spec.Instructions
	case *Rule:
		return v.Spec.Instructions
	default:
		return ""
	}
}
