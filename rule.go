package warp

import (
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// RuleRenderOptions holds options for rendering rule instructions.
type RuleRenderOptions struct {
	Workspace *Workspace
	Project   *Project
	Agent     *Agent // Optional invoking agent
	Globals   map[string]any
}

// Render processes the rule's instructions as a template.
// It acts as a rich extension that injects Workspace, Project, and optional invoking
// Agent into the generic Render.
func (r *Rule) Render(opts *RuleRenderOptions) (string, error) {
	if r == nil {
		return "", nil
	}
	if opts == nil {
		opts = &RuleRenderOptions{}
	}

	mergedGlobals := make(map[string]any)
	maps.Copy(mergedGlobals, opts.Globals)

	// Inject Workspace derived from arguments
	if opts.Workspace != nil {
		tw := TemplateWorkspace{
			Dir:  opts.Workspace.RootPath,
			Path: filepath.Join(opts.Workspace.RootPath, WorkspaceFileName),
		}
		mergedGlobals["Workspace"] = tw
		mergedGlobals["WorkspaceDir"] = tw.Dir
		mergedGlobals["WorkspacePath"] = tw.Path
	} else {
		mergedGlobals["Workspace"] = TemplateWorkspace{}
		mergedGlobals["WorkspaceDir"] = ""
		mergedGlobals["WorkspacePath"] = ""
	}

	// Inject Project derived from arguments
	if opts.Project != nil {
		var displayName string
		if opts.Project.Context != nil && opts.Project.Context.Metadata.DisplayName != "" {
			displayName = opts.Project.Context.Metadata.DisplayName
		} else {
			displayName = opts.Project.Name
		}

		tp := TemplateProject{
			Name:        opts.Project.Name,
			DisplayName: displayName,
			Dir:         opts.Project.AbsPath(),
		}
		mergedGlobals["Project"] = tp
		mergedGlobals["ProjectDir"] = tp.Dir

		if opts.Project.Context != nil {
			tc := TemplateContext{
				Instructions: opts.Project.Context.Spec.Instructions,
				Path:         filepath.Join(opts.Project.Context.Directory, ContextFileName),
			}
			mergedGlobals["Context"] = tc
			mergedGlobals["ContextPath"] = tc.Path
		}
	} else {
		mergedGlobals["Project"] = TemplateProject{}
		mergedGlobals["ProjectDir"] = ""
	}

	// Inject invoking Agent derived from arguments
	if opts.Agent != nil {
		ta := TemplateAgent{
			Name:        opts.Agent.Metadata.Name,
			Description: opts.Agent.Metadata.Description,
			Dir:         opts.Agent.Directory,
			Path:        filepath.Join(opts.Agent.Directory, opts.Agent.Metadata.Name+".md"),
		}
		mergedGlobals["Agent"] = ta
	} else {
		mergedGlobals["Agent"] = TemplateAgent{}
	}

	renderOpts := &RenderOptions{
		Globals: mergedGlobals,
	}

	return Render(r, renderOpts)
}

// Rule is a warp resource that defines ambient constraints, coding standards,
// security guardrails, or architectural invariants that agents must follow.
type Rule struct {
	BaseResource `yaml:",inline"`
	// Spec holds the rule-specific configuration.
	Spec RuleSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Rule.
func (r *Rule) DeepCopy() *Rule {
	if r == nil {
		return nil
	}
	out := new(Rule)
	out.BaseResource = *r.BaseResource.DeepCopy()
	out.Spec = *r.Spec.DeepCopy()
	return out
}

// RuleSpec contains the configuration details for a Rule resource.
type RuleSpec struct {
	// Triggers defines the activation conditions for this rule.
	Triggers RuleTriggers `yaml:"triggers,omitempty"`
	// Priority controls the ordering when multiple rules are resolved and injected.
	// Higher numbers take precedence. Default is 0.
	Priority int `yaml:"priority,omitempty"`
	// Instructions is the directive text populated from the Markdown body
	// of the file (below the closing front-matter delimiter).
	Instructions string `yaml:"instructions,omitempty"`
}

// DeepCopy returns a deep copy of the RuleSpec.
func (in *RuleSpec) DeepCopy() *RuleSpec {
	if in == nil {
		return nil
	}
	out := new(RuleSpec)
	*out = *in
	out.Triggers = *in.Triggers.DeepCopy()
	return out
}

// RuleTriggers specifies when a rule becomes active.
type RuleTriggers struct {
	// Always indicates whether this rule applies unconditionally within its scope.
	Always bool `yaml:"always,omitempty"`
	// Globs is a list of file path patterns (e.g. ["**/*.go", "!**/vendor/**"])
	// that trigger this rule when relevant files are targeted or edited.
	Globs []string `yaml:"globs,omitempty,flow"`
	// UseWhen provides semantic instructions to an LLM or router on when this rule applies.
	UseWhen string `yaml:"useWhen,omitempty"`
	// Keywords is an array of semantic tags used for discovery and prompting.
	Keywords []string `yaml:"keywords,omitempty,flow"`
	// Decision is an optional reference to a Decision resource evaluated to activate this rule.
	Decision string `yaml:"decision,omitempty"`
	// Tools is a list of Tool or Toolkit names that trigger this rule when invoked.
	Tools []string `yaml:"tools,omitempty,flow"`
}

// DeepCopy returns a deep copy of the RuleTriggers.
func (r *RuleTriggers) DeepCopy() *RuleTriggers {
	if r == nil {
		return nil
	}
	out := new(RuleTriggers)
	*out = *r
	if r.Globs != nil {
		out.Globs = make([]string, len(r.Globs))
		copy(out.Globs, r.Globs)
	}
	if r.Keywords != nil {
		out.Keywords = make([]string, len(r.Keywords))
		copy(out.Keywords, r.Keywords)
	}
	if r.Tools != nil {
		out.Tools = make([]string, len(r.Tools))
		copy(out.Tools, r.Tools)
	}
	return out
}

// MatchesPath reports whether the trigger conditions match the target file path.
func (r *RuleTriggers) MatchesPath(targetPath string) bool {
	if r == nil {
		return false
	}
	if r.Always {
		return true
	}
	if len(r.Globs) == 0 {
		return false
	}

	var hasPositive bool
	for _, pat := range r.Globs {
		if after, ok := strings.CutPrefix(pat, "!"); ok {
			negPat := after
			if matchGlobPath(negPat, targetPath) {
				return false
			}
		} else {
			hasPositive = true
		}
	}

	if !hasPositive {
		return true
	}

	for _, pat := range r.Globs {
		if !strings.HasPrefix(pat, "!") {
			if matchGlobPath(pat, targetPath) {
				return true
			}
		}
	}

	return false
}

// MatchesTool reports whether the trigger conditions match the given tool or toolkit name.
func (r *RuleTriggers) MatchesTool(toolName string) bool {
	if r == nil || len(r.Tools) == 0 {
		return false
	}
	for _, tool := range r.Tools {
		if tool == "*" || tool == toolName {
			return true
		}
		if hasGlobMeta(tool) {
			if m, _ := filepath.Match(tool, toolName); m {
				return true
			}
		}
	}
	return false
}

// matchGlobPath tests if targetPath matches pattern, supporting recursive "**",
// wildcards, and base-name fallback matching.
func matchGlobPath(pattern, targetPath string) bool {
	p := filepath.ToSlash(targetPath)
	p = strings.TrimPrefix(p, "./")
	pattern = filepath.ToSlash(pattern)
	pattern = strings.TrimPrefix(pattern, "./")

	// If no slash in pattern, match either base name or full path
	if !strings.Contains(pattern, "/") {
		if m, _ := filepath.Match(pattern, path.Base(p)); m {
			return true
		}
	}

	rx, err := globToRegex(pattern)
	if err != nil {
		m, _ := filepath.Match(pattern, p)
		return m
	}
	return rx.MatchString(p)
}

// globToRegex converts a path glob pattern into a compiled regular expression.
func globToRegex(pattern string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("^")
	i := 0
	n := len(pattern)
	for i < n {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < n && pattern[i+1] == '*' {
				i += 2
				if i < n && pattern[i] == '/' {
					i++
					sb.WriteString("(?:.*/)?")
				} else {
					sb.WriteString(".*")
				}
			} else {
				i++
				sb.WriteString("[^/]*")
			}
		case '?':
			i++
			sb.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '{', '}', '[', ']', '^', '$', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
			i++
		default:
			sb.WriteByte(c)
			i++
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}
