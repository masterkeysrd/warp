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
	Models      []string
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
	UseWhen     string
	Keywords    []string
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
	Tools        []string
	ArgumentHint string
	Arguments    []Argument
	Dir          string
	Path         string
}

// TemplateCommand represents the command view model for templates.
type TemplateCommand struct {
	Name         string
	Description  string
	Agent        string
	Context      CommandContextMode
	ArgumentHint string
	Arguments    []Argument
	Dir          string
	Path         string
	Tools        []TemplateTool
	Skills       []TemplateSkill
	Rules        []TemplateRule
}

// TemplateDecision represents the decision view model for templates.
type TemplateDecision struct {
	Name         string
	Description  string
	ArgumentHint string
	Arguments    []Argument
	Questions    []DecisionQuestion
	Dir          string
	Path         string
}

// TemplateTool represents the tool view model for templates.
type TemplateTool struct {
	Name        string
	Description string
	Dir         string
	Path        string
}

// TemplateMCP represents the MCP server view model for templates.
type TemplateMCP struct {
	Name        string
	Description string
	Type        string
	Endpoint    string
	Dir         string
	Path        string
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

var (
	reShorthandBraceNum = regexp.MustCompile(`\$\{([0-9]+)\}`)
	reShorthandBraceVar = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)
	reShorthandVarNum   = regexp.MustCompile(`\$([0-9]+)`)
	reShorthandVar      = regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z0-9_]*)`)
)

// preprocessShorthand converts shell-style variables outside Go text/template
// {{ ... }} actions to Go template variables. Handles $var, ${var}, $1, ${1},
// and escapes $$, while leaving native template variables ($i, $v, $.Field)
// inside {{ ... }} untouched.
func preprocessShorthand(input string) string {
	if !strings.Contains(input, "$") {
		return input
	}

	var out strings.Builder
	out.Grow(len(input))
	pos := 0
	for pos < len(input) {
		rel := strings.Index(input[pos:], "{{")
		if rel < 0 {
			out.WriteString(expandShorthand(input[pos:]))
			break
		}
		if rel > 0 {
			out.WriteString(expandShorthand(input[pos : pos+rel]))
		}
		actionStart := pos + rel
		actionEnd := findTemplateActionEnd(input, actionStart)
		if actionEnd < 0 {
			out.WriteString(input[actionStart:])
			break
		}
		out.WriteString(input[actionStart:actionEnd])
		pos = actionEnd
	}
	return out.String()
}

func expandShorthand(segment string) string {
	if !strings.Contains(segment, "$") {
		return segment
	}

	// 1. Escape literal $$
	s := strings.ReplaceAll(segment, "$$", "%%ESCAPED_DOLLAR%%")

	// 2. Replace ${var} -> {{.var}} (or {{index . "var"}} if it's a number)
	s = reShorthandBraceNum.ReplaceAllString(s, `{{index . "$1"}}`)
	s = reShorthandBraceVar.ReplaceAllString(s, "{{.$1}}")

	// 3. Replace $var or $1
	s = reShorthandVarNum.ReplaceAllString(s, `{{index . "$1"}}`)
	s = reShorthandVar.ReplaceAllString(s, "{{.$1}}")

	// 4. Restore literal $
	return strings.ReplaceAll(s, "%%ESCAPED_DOLLAR%%", "$")
}

// findTemplateActionEnd returns the index immediately after the closing "}}"
// of the Go text/template action starting at start, respecting string literals,
// raw strings, rune literals, and template comments. Returns -1 if unclosed.
func findTemplateActionEnd(s string, start int) int {
	i := start + 2

	// Handle template comments: {{/* ... */}} or {{- /* ... */ -}}
	j := i
	if j < len(s) && s[j] == '-' {
		j++
	}
	for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r' || s[j] == '\n') {
		j++
	}
	if strings.HasPrefix(s[j:], "/*") {
		commentEnd := strings.Index(s[j+2:], "*/")
		if commentEnd < 0 {
			return -1
		}
		i = j + 2 + commentEnd + 2
	}

	for i < len(s) {
		switch s[i] {
		case '"':
			i++
			for i < len(s) {
				if s[i] == '\\' {
					i += 2
					continue
				}
				if s[i] == '"' {
					i++
					break
				}
				i++
			}
		case '`':
			i++
			for i < len(s) && s[i] != '`' {
				i++
			}
			if i < len(s) {
				i++
			}
		case '\'':
			i++
			for i < len(s) {
				if s[i] == '\\' {
					i += 2
					continue
				}
				if s[i] == '\'' {
					i++
					break
				}
				i++
			}
		case '}':
			if i+1 < len(s) && s[i+1] == '}' {
				return i + 2
			}
			i++
		default:
			i++
		}
	}
	return -1
}

// buildTemplateData lifts fields from the resource to the top level.
func buildTemplateData(res Resource, opts *RenderOptions) (map[string]any, error) {
	data := make(map[string]any)

	// Initialize built-in shorthands to empty string to avoid <no value> outputs
	data["WorkspaceDir"] = ""
	data["WorkspacePath"] = ""
	data["ProjectDir"] = ""
	data["ContextPath"] = ""
	data["RuleDir"] = ""
	data["RulePath"] = ""
	data["SkillDir"] = ""
	data["SkillPath"] = ""
	data["CommandDir"] = ""
	data["CommandPath"] = ""
	data["DecisionDir"] = ""
	data["DecisionPath"] = ""
	data["ToolDir"] = ""
	data["ToolPath"] = ""
	data["MCPDir"] = ""
	data["MCPPath"] = ""

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
			ctxPath := v.GetPath()
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
				Path: v.GetPath(),
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
				Models:      v.Spec.Models,
				Dir:         v.Directory,
				Path:        v.GetPath(),
			}
			data["Agent"] = ta
		}
		if _, ok := data["Models"]; !ok {
			data["Models"] = v.Spec.Models
		}
		if _, ok := data["Skills"]; !ok {
			data["Skills"] = v.Spec.Skills
		}
		if _, ok := data["Commands"]; !ok {
			data["Commands"] = v.Spec.Commands
		}
		if _, ok := data["Subagents"]; !ok {
			data["Subagents"] = v.Spec.Subagents
		}
		if _, ok := data["Rules"]; !ok {
			data["Rules"] = v.Spec.Rules
		}
		data["Triggers"] = v.Spec.Triggers
		if v.Spec.Temperature != nil {
			data["Temperature"] = *v.Spec.Temperature
		} else {
			data["Temperature"] = 0.0
		}
	case *Skill:
		ts := TemplateSkill{
			Name:         v.Metadata.Name,
			Description:  v.Metadata.Description,
			UseWhen:      v.Spec.UseWhen,
			Keywords:     v.Spec.Keywords,
			Tools:        v.Spec.Tools,
			ArgumentHint: v.Spec.ArgumentHint,
			Arguments:    v.Spec.Arguments,
			Dir:          v.Directory,
			Path:         v.GetPath(),
		}
		if _, ok := data["Skill"]; !ok {
			data["Skill"] = ts
		}
		data["SkillDir"] = ts.Dir
		data["SkillPath"] = ts.Path
		data["UseWhen"] = v.Spec.UseWhen
		data["Keywords"] = v.Spec.Keywords
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
				} else {
					data[arg.Name] = ""
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
		ctxMode := v.Spec.Context
		if ctxMode == "" {
			ctxMode = CommandContextInline
		}
		var tc TemplateCommand
		if existing, ok := data["Command"].(TemplateCommand); ok {
			tc = existing
		} else {
			tc = TemplateCommand{
				Name:         v.Metadata.Name,
				Description:  v.Metadata.Description,
				Agent:        v.Spec.Agent,
				Context:      ctxMode,
				ArgumentHint: v.Spec.ArgumentHint,
				Arguments:    v.Spec.Arguments,
				Dir:          v.Directory,
				Path:         v.GetPath(),
			}
			data["Command"] = tc
		}
		data["CommandDir"] = tc.Dir
		data["CommandPath"] = tc.Path
		data["Skills"] = v.Spec.Skills
		data["Rules"] = v.Spec.Rules
		data["Models"] = v.Spec.Models
		data["Tools"] = v.Spec.Tools
		data["ArgumentHint"] = v.Spec.ArgumentHint
		data["Arguments"] = v.Spec.Arguments
		if v.Spec.Agent != "" {
			data["CommandAgent"] = v.Spec.Agent
		}
		data["CommandContext"] = string(ctxMode)

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
				} else {
					data[arg.Name] = ""
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
		var td TemplateDecision
		if existing, ok := data["Decision"].(TemplateDecision); ok {
			td = existing
		} else {
			td = TemplateDecision{
				Name:         v.Metadata.Name,
				Description:  v.Metadata.Description,
				ArgumentHint: v.Spec.ArgumentHint,
				Arguments:    v.Spec.Arguments,
				Dir:          v.Directory,
				Path:         v.GetPath(),
			}
		}
		td.Questions = v.Spec.Questions
		data["Decision"] = td
		data["DecisionDir"] = td.Dir
		data["DecisionPath"] = td.Path
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
				} else {
					data[arg.Name] = ""
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
				UseWhen:     v.Spec.Triggers.UseWhen,
				Keywords:    v.Spec.Triggers.Keywords,
				Dir:         v.Directory,
				Path:        v.GetPath(),
				Priority:    v.Spec.Priority,
			}
			data["Rule"] = tr
			data["RuleDir"] = tr.Dir
			data["RulePath"] = tr.Path
		}
		data["UseWhen"] = v.Spec.Triggers.UseWhen
		data["Keywords"] = v.Spec.Triggers.Keywords
		data["Priority"] = v.Spec.Priority
		data["Triggers"] = v.Spec.Triggers
	case *Tool:
		var tt TemplateTool
		if existing, ok := data["Tool"].(TemplateTool); ok {
			tt = existing
			if tt.Dir == "" {
				tt.Dir = v.Directory
			}
			if tt.Path == "" {
				tt.Path = v.GetPath()
			}
			if tt.Description == "" {
				tt.Description = v.Metadata.Description
			}
			data["Tool"] = tt
		} else {
			tt = TemplateTool{
				Name:        v.Metadata.Name,
				Description: v.Metadata.Description,
				Dir:         v.Directory,
				Path:        v.GetPath(),
			}
			data["Tool"] = tt
		}
		data["ToolDir"] = tt.Dir
		data["ToolPath"] = tt.Path
		data["Command"] = v.Spec.Command
		data["Env"] = v.Spec.Env
		data["InputSchema"] = v.Spec.InputSchema
		data["OutputSchema"] = v.Spec.OutputSchema
		data["Annotations"] = v.Spec.Annotations
	case *MCP:
		var tm TemplateMCP
		if existing, ok := data["MCP"].(TemplateMCP); ok {
			tm = existing
			if tm.Dir == "" {
				tm.Dir = v.Directory
			}
			if tm.Path == "" {
				tm.Path = v.GetPath()
			}
			if tm.Description == "" {
				tm.Description = v.Metadata.Description
			}
			if tm.Type == "" {
				tm.Type = string(v.Spec.Type)
			}
			if tm.Endpoint == "" {
				tm.Endpoint = v.Spec.Endpoint
			}
			data["MCP"] = tm
		} else {
			tm = TemplateMCP{
				Name:        v.Metadata.Name,
				Description: v.Metadata.Description,
				Type:        string(v.Spec.Type),
				Endpoint:    v.Spec.Endpoint,
				Dir:         v.Directory,
				Path:        v.GetPath(),
			}
			data["MCP"] = tm
		}
		data["MCPDir"] = tm.Dir
		data["MCPPath"] = tm.Path
		data["Command"] = v.Spec.Command
		data["Endpoint"] = v.Spec.Endpoint
		data["Env"] = v.Spec.Env
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
	case *Tool:
		return v.Spec.Instructions
	case *MCP:
		return ""
	default:
		return ""
	}
}
