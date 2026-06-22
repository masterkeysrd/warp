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
	Path string
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
}

// TemplateSkill represents the skill view model for templates.
type TemplateSkill struct {
	Name        string
	Description string
	Dir         string
	Path        string
}

// TemplateCommand represents the command view model for templates.
type TemplateCommand struct {
	Name        string
	Description string
	Dir         string
	Path        string
	Tools       []TemplateTool
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

	data := buildTemplateData(res, opts)
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
func buildTemplateData(res Resource, opts *RenderOptions) map[string]any {
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
		data["Models"] = v.Spec.Models
		data["Skills"] = v.Spec.Skills
		data["Commands"] = v.Spec.Commands
		data["Triggers"] = v.Spec.Triggers
		data["Temperature"] = v.Spec.Temperature
	case *Skill:
		if _, ok := data["Skill"]; !ok {
			ts := TemplateSkill{
				Name:        v.Metadata.Name,
				Description: v.Metadata.Description,
				Dir:         v.Directory,
				Path:        filepath.Join(v.Directory, v.Metadata.Name+".md"),
			}
			data["Skill"] = ts
		}
	case *Command:
		if _, ok := data["Command"]; !ok {
			tc := TemplateCommand{
				Name:        v.Metadata.Name,
				Description: v.Metadata.Description,
				Dir:         v.Directory,
				Path:        filepath.Join(v.Directory, v.Metadata.Name+".md"),
			}
			data["Command"] = tc
		}
		data["Models"] = v.Spec.Models
		data["Tools"] = v.Spec.Tools
		data["Hints"] = v.Spec.Hints

		// Map arguments
		if len(opts.Args) > 0 {
			// Provide `.Args` as a slice for complex usage
			data["Args"] = opts.Args

			// Positional mappings: $1, $2 (note: 1-indexed for templates to match bash style)
			for i, arg := range opts.Args {
				data[fmt.Sprintf("%d", i+1)] = arg
			}

			// Hint mappings
			for i, hint := range v.Spec.Hints {
				if i < len(opts.Args) {
					data[hint] = opts.Args[i]
				}
			}
		}
	}

	return data
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
	default:
		return ""
	}
}
