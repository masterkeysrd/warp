package warp

import (
	"maps"
	"path/filepath"
)

// AgentRenderOptions holds options for rendering agent instructions.
type AgentRenderOptions struct {
	Workspace *Workspace
	Project   *Project
	Resolved  *ResolvedAgent // Optional, contains fully hydrated Tools, Skills, Commands
	Globals   map[string]any
}

// Render processes the agent's instructions as a template.
// It acts as a rich extension that injects Workspace, Project, and fully hydrated
// Agent metadata into the generic Render.
func (a *Agent) Render(opts *AgentRenderOptions) (string, error) {
	if a == nil {
		return "", nil
	}
	if opts == nil {
		opts = &AgentRenderOptions{}
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

	// Inject Agent derived from Resolved (if available) or base
	var ta TemplateAgent
	if opts.Resolved != nil && opts.Resolved.Agent != nil {
		ta = TemplateAgent{
			Name:        opts.Resolved.Agent.Metadata.Name,
			Description: opts.Resolved.Agent.Metadata.Description,
			Models:      opts.Resolved.Agent.Spec.Models,
			Dir:         opts.Resolved.Agent.Directory,
			Path:        opts.Resolved.Agent.GetPath(),
		}
		for _, s := range opts.Resolved.Skills {
			ta.Skills = append(ta.Skills, TemplateSkill{
				Name:         s.Metadata.Name,
				Description:  s.Metadata.Description,
				UseWhen:      s.Spec.UseWhen,
				Keywords:     s.Spec.Keywords,
				Tools:        s.Spec.Tools,
				ArgumentHint: s.Spec.ArgumentHint,
				Arguments:    s.Spec.Arguments,
				Dir:          s.Directory,
				Path:         s.GetPath(),
			})
		}
		for _, t := range opts.Resolved.Tools {
			ta.Tools = append(ta.Tools, TemplateTool{
				Name:        t.Metadata.Name,
				Description: t.Metadata.Description,
				Dir:         t.Directory,
				Path:        t.GetPath(),
			})
		}
		for _, cmd := range opts.Resolved.Commands {
			ctxMode := cmd.Spec.Context
			if ctxMode == "" {
				ctxMode = CommandContextInline
			}
			ta.Commands = append(ta.Commands, TemplateCommand{
				Name:         cmd.Metadata.Name,
				Description:  cmd.Metadata.Description,
				Agent:        cmd.Spec.Agent,
				Context:      ctxMode,
				ArgumentHint: cmd.Spec.ArgumentHint,
				Arguments:    cmd.Spec.Arguments,
				Dir:          cmd.Directory,
				Path:         cmd.GetPath(),
			})
		}
		for _, sa := range opts.Resolved.Subagents {
			ta.Subagents = append(ta.Subagents, TemplateAgent{
				Name:        sa.Metadata.Name,
				Description: sa.Metadata.Description,
				Models:      sa.Spec.Models,
				Dir:         sa.Directory,
				Path:        sa.GetPath(),
			})
		}

		var subagentNames []string
		for _, sa := range opts.Resolved.Subagents {
			subagentNames = append(subagentNames, sa.Metadata.Name)
		}
		mergedGlobals["Subagents"] = subagentNames

		var skillNames []string
		for _, s := range opts.Resolved.Skills {
			skillNames = append(skillNames, s.Metadata.Name)
		}
		mergedGlobals["Skills"] = skillNames

		var cmdNames []string
		for _, cmd := range opts.Resolved.Commands {
			cmdNames = append(cmdNames, cmd.Metadata.Name)
		}
		mergedGlobals["Commands"] = cmdNames

		var toolNames []string
		for _, t := range opts.Resolved.Tools {
			toolNames = append(toolNames, t.Metadata.Name)
		}
		mergedGlobals["Tools"] = toolNames

		for _, rl := range opts.Resolved.Rules {
			ta.Rules = append(ta.Rules, TemplateRule{
				Name:        rl.Metadata.Name,
				Description: rl.Metadata.Description,
				UseWhen:     rl.Spec.Triggers.UseWhen,
				Keywords:    rl.Spec.Triggers.Keywords,
				Dir:         rl.Directory,
				Path:        rl.GetPath(),
				Priority:    rl.Spec.Priority,
			})
		}

		var ruleNames []string
		for _, rl := range opts.Resolved.Rules {
			ruleNames = append(ruleNames, rl.Metadata.Name)
		}
		mergedGlobals["Rules"] = ruleNames
	} else {
		ta = TemplateAgent{
			Name:        a.Metadata.Name,
			Description: a.Metadata.Description,
			Models:      a.Spec.Models,
			Dir:         a.Directory,
			Path:        a.GetPath(),
		}
	}
	mergedGlobals["Agent"] = ta

	renderOpts := &RenderOptions{
		Globals: mergedGlobals,
	}

	return Render(a, renderOpts)
}

// Agent is a warp resource that describes an autonomous agent: its LLM
// configuration, persona instructions, and the set of skills and commands it
// may invoke at runtime.
type Agent struct {
	BaseResource `yaml:",inline"`
	// Spec holds the agent-specific configuration.
	Spec AgentSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Agent.
func (a *Agent) DeepCopy() *Agent {
	if a == nil {
		return nil
	}
	out := new(Agent)
	out.BaseResource = *a.BaseResource.DeepCopy()
	out.Spec = *a.Spec.DeepCopy()
	return out
}

// Standard architectural triggers that invoke an agent.
const (
	TriggerHuman  = "human"
	TriggerAgent  = "agent"
	TriggerSystem = "system"
)

// ThinkingStrategy defines reasoning configuration strategies.
type ThinkingStrategy string

const (
	ThinkingStrategyToggle   ThinkingStrategy = "toggle"
	ThinkingStrategyEffort   ThinkingStrategy = "effort"
	ThinkingStrategyBudget   ThinkingStrategy = "budget"
	ThinkingStrategyAdaptive ThinkingStrategy = "adaptive"
)

// AgentThinking specifies runtime configuration for extended reasoning.
type AgentThinking struct {
	Type         ThinkingStrategy `yaml:"type" json:"type"`
	Enabled      *bool            `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Effort       string           `yaml:"effort,omitempty" json:"effort,omitempty"`
	BudgetTokens int              `yaml:"budgetTokens,omitempty" json:"budgetTokens,omitempty"`
}

// DeepCopy returns a deep copy of the AgentThinking.
func (in *AgentThinking) DeepCopy() *AgentThinking {
	if in == nil {
		return nil
	}
	out := new(AgentThinking)
	*out = *in
	if in.Enabled != nil {
		e := *in.Enabled
		out.Enabled = &e
	}
	return out
}

// AgentSpec contains the configuration details for an Agent resource.
type AgentSpec struct {
	// Extends is the Qualified Name or Short Name of another Agent resource to
	// inherit from. When set, the engine merges the parent's skills and tools
	// arrays (parent first) and prepends the parent's instructions to this
	// agent's instructions.
	Extends string `yaml:"extends,omitempty"`
	// Instructions is the persona prompt populated from the Markdown body of
	// the file (below the closing front-matter delimiter).
	Instructions string `yaml:"instructions,omitempty"`
	// Triggers defines the architectural constraints on what can invoke this agent
	// (e.g., "human", "agent"). An empty list means the agent can be triggered
	// by anything.
	Triggers []string `yaml:"triggers,omitempty,flow"`
	// Models is a prioritized list of allowed LLM model identifiers or aliases
	// (e.g., ["@default", "@fast"] or ["gpt-4o", "claude-3-5-sonnet"]). The
	// runtime uses the first available model by default, and when non-empty,
	// restricts caller-requested model overrides to this list.
	Models []string `yaml:"models,omitempty,flow"`
	// Temperature controls the randomness of the model's output (0.0–2.0).
	Temperature *float64 `yaml:"temperature,omitempty"`
	// Thinking specifies the runtime configuration for extended reasoning (e.g., type: effort, effort: high). Overrides the provider's default.
	Thinking *AgentThinking `yaml:"thinking,omitempty"`
	// Skills is a list of Skill references (Qualified or Short Names) or globs
	// this agent is allowed to use.
	Skills []string `yaml:"skills,omitempty"`
	// Commands is a list of Command references (Qualified or Short Names) or globs
	// this agent can invoke.
	Commands []string `yaml:"commands,omitempty"`
	// Subagents is a list of Agent references (Qualified or Short Names) or globs
	// this agent is allowed to delegate to.
	Subagents []string `yaml:"subagents,omitempty"`
	// Rules is an optional list of Rule references (Qualified or Short Names) or
	// globs this agent is governed by. Attached rules act as always-on rules for this agent.
	Rules []string `yaml:"rules,omitempty"`
	// Tools is a list of Tool references (Qualified or Short Names) or globs
	// loaded into the agent's base context. Default is [] (none). Use ["*"] to
	// include all effective tools in scope.
	Tools []string `yaml:"tools,omitempty"`
	// Policies defines security and access policies.
	Policies *Policies `yaml:"policies,omitempty"`
}

// DeepCopy returns a deep copy of the AgentSpec.
func (in *AgentSpec) DeepCopy() *AgentSpec {
	if in == nil {
		return nil
	}
	out := new(AgentSpec)
	*out = *in
	if in.Triggers != nil {
		out.Triggers = make([]string, len(in.Triggers))
		copy(out.Triggers, in.Triggers)
	}
	if in.Models != nil {
		out.Models = make([]string, len(in.Models))
		copy(out.Models, in.Models)
	}
	if in.Temperature != nil {
		t := *in.Temperature
		out.Temperature = &t
	}
	if in.Thinking != nil {
		out.Thinking = in.Thinking.DeepCopy()
	}
	if in.Skills != nil {
		out.Skills = make([]string, len(in.Skills))
		copy(out.Skills, in.Skills)
	}
	if in.Commands != nil {
		out.Commands = make([]string, len(in.Commands))
		copy(out.Commands, in.Commands)
	}
	if in.Subagents != nil {
		out.Subagents = make([]string, len(in.Subagents))
		copy(out.Subagents, in.Subagents)
	}
	if in.Rules != nil {
		out.Rules = make([]string, len(in.Rules))
		copy(out.Rules, in.Rules)
	}
	if in.Tools != nil {
		out.Tools = make([]string, len(in.Tools))
		copy(out.Tools, in.Tools)
	}
	if in.Policies != nil {
		out.Policies = in.Policies.DeepCopy()
	}
	return out
}
