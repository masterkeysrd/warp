package warp

import (
	"bytes"
	"fmt"
	"maps"
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

// Render processes the resource's instructions as a template.
// It supports both standard Go text/template syntax ({{.Name}}) and a
// convenient shorthand syntax ($Name, $1).
func Render(res Resource, opts *RenderOptions) (string, error) {
	if opts == nil {
		opts = &RenderOptions{}
	}

	rawInstructions := getInstructions(res)
	if rawInstructions == "" {
		return "", nil
	}

	// 1. Build the flattened data context
	data := buildTemplateData(res, opts)

	// 2. Preprocess shorthand $Var to {{.Var}}
	tmplStr := preprocessShorthand(rawInstructions)

	// 3. Parse and execute the template
	t, err := template.New(res.GetName()).Option("missingkey=zero").Parse(tmplStr)
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
	case *Agent:
		data["Models"] = v.Spec.Models
		data["Skills"] = v.Spec.Skills
		data["Tools"] = v.Spec.Tools
		data["Commands"] = v.Spec.Commands
		data["Triggers"] = v.Spec.Triggers
		data["Temperature"] = v.Spec.Temperature
	case *Command:
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
				// Offset by 1 to get the argument *after* the positional command name,
				// if we treat args[0] as $1.
				// Wait, the test passes `[]string{"Pos1", "HintVal1", "HintVal2"}`.
				// If hints are `["hint1", "hint2"]`, hint1 should map to args[1]?
				// Ah, no. If hints are ["ticker", "year"], then $ticker = $1, $year = $2.
				// So hint `i` corresponds to `opts.Args[i]`.
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
