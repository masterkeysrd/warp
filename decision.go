package warp

import (
	"maps"
	"path/filepath"
)

// DecisionQuestionType identifies the evaluation mode of a DecisionQuestion.
type DecisionQuestionType string

const (
	// DecisionQuestionPredicate evaluates a binary true/false condition.
	DecisionQuestionPredicate DecisionQuestionType = "predicate"
	// DecisionQuestionChoice selects among discrete categorical options.
	DecisionQuestionChoice DecisionQuestionType = "choice"
	// DecisionQuestionScore rates an input across ordered rubric levels.
	DecisionQuestionScore DecisionQuestionType = "score"
)

// DecisionRenderOptions holds options for rendering decision instructions.
type DecisionRenderOptions struct {
	Workspace *Workspace
	Project   *Project
	Args      []string
	Globals   map[string]any
}

// Render processes the decision's instructions as a template.
// It injects Workspace, Project scope, and positional Args into the generic Render.
func (d *Decision) Render(opts *DecisionRenderOptions) (string, error) {
	if d == nil {
		return "", nil
	}
	if opts == nil {
		opts = &DecisionRenderOptions{}
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

	renderOpts := &RenderOptions{
		Args:    opts.Args,
		Globals: mergedGlobals,
	}

	return Render(d, renderOpts)
}

// Decision is a warp resource that encapsulates a probabilistic decision
// evaluation (such as binary predicates, categorical choices, and ordinal
// scoring rubrics). Its general evaluation context or rubric is authored as
// the Markdown body of the defining file.
type Decision struct {
	BaseResource `yaml:",inline"`
	// Spec holds the decision-specific configuration.
	Spec DecisionSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Decision.
func (d *Decision) DeepCopy() *Decision {
	if d == nil {
		return nil
	}
	out := new(Decision)
	out.BaseResource = *d.BaseResource.DeepCopy()
	out.Spec = *d.Spec.DeepCopy()
	return out
}

// DecisionSpec contains the configuration details for a Decision resource.
type DecisionSpec struct {
	// Instructions is the evaluation prompt/rubric populated from the Markdown
	// body of the file (below the closing front-matter delimiter).
	Instructions string `yaml:"instructions,omitempty"`
	// Models is a prioritized list of decision model identifiers to use
	// (e.g., ["typesafe/jev-latest", "ollama/nimble"]). Overrides workspace/provider defaults.
	Models []string `yaml:"models,omitempty,flow"`
	// Hints is an ordered list of argument hints that UIs and runtimes can use
	// for positional template substitution in Instructions.
	Hints []string `yaml:"hints,omitempty"`
	// Questions is the list of structured questions (predicates, choices, scores)
	// evaluated by this Decision. Optional when questions are supplied dynamically at runtime.
	Questions []DecisionQuestion `yaml:"questions,omitempty"`
}

// DeepCopy returns a deep copy of the DecisionSpec.
func (in *DecisionSpec) DeepCopy() *DecisionSpec {
	if in == nil {
		return nil
	}
	out := new(DecisionSpec)
	*out = *in
	if in.Models != nil {
		out.Models = make([]string, len(in.Models))
		copy(out.Models, in.Models)
	}
	if in.Hints != nil {
		out.Hints = make([]string, len(in.Hints))
		copy(out.Hints, in.Hints)
	}
	if in.Questions != nil {
		out.Questions = make([]DecisionQuestion, len(in.Questions))
		for i, q := range in.Questions {
			out.Questions[i] = *q.DeepCopy()
		}
	}
	return out
}

// DecisionQuestion defines a single structured question within a Decision resource.
type DecisionQuestion struct {
	// Name is the unique identifier for this question within the Decision.
	Name string `yaml:"name"`
	// Type is the question evaluation mode: "predicate", "choice", or "score".
	Type DecisionQuestionType `yaml:"type"`
	// Instructions describes the specific question or condition to evaluate.
	Instructions string `yaml:"instructions,omitempty"`
	// Threshold is an optional minimum probability [0.0, 1.0] required for a predicate to pass.
	Threshold *float64 `yaml:"threshold,omitempty"`
	// MinConfidence is an optional minimum confidence [0.0, 1.0] required for an answer.
	MinConfidence *float64 `yaml:"minConfidence,omitempty"`
	// Criteria defines optional positive and negative evaluation criteria for a predicate question.
	Criteria *DecisionCriteria `yaml:"criteria,omitempty"`
	// Options defines the candidate choices when type is "choice".
	Options []DecisionOption `yaml:"options,omitempty"`
	// Levels defines the ordered rubric levels when type is "score".
	Levels []DecisionLevel `yaml:"levels,omitempty"`
}

// DeepCopy returns a deep copy of the DecisionQuestion.
func (in *DecisionQuestion) DeepCopy() *DecisionQuestion {
	if in == nil {
		return nil
	}
	out := new(DecisionQuestion)
	*out = *in
	if in.Threshold != nil {
		v := *in.Threshold
		out.Threshold = &v
	}
	if in.MinConfidence != nil {
		v := *in.MinConfidence
		out.MinConfidence = &v
	}
	if in.Criteria != nil {
		out.Criteria = in.Criteria.DeepCopy()
	}
	if in.Options != nil {
		out.Options = make([]DecisionOption, len(in.Options))
		copy(out.Options, in.Options)
	}
	if in.Levels != nil {
		out.Levels = make([]DecisionLevel, len(in.Levels))
		copy(out.Levels, in.Levels)
	}
	return out
}

// DecisionCriteria defines explicit positive and negative criteria for a predicate question.
type DecisionCriteria struct {
	True  string `yaml:"true,omitempty"`  // Description of when the predicate evaluates to true
	False string `yaml:"false,omitempty"` // Description of when the predicate evaluates to false
}

// DeepCopy returns a deep copy of the DecisionCriteria.
func (in *DecisionCriteria) DeepCopy() *DecisionCriteria {
	if in == nil {
		return nil
	}
	out := new(DecisionCriteria)
	*out = *in
	return out
}

// DecisionOption defines a single categorical choice option.
type DecisionOption struct {
	Value       string `yaml:"value"`                 // Identifier returned when this option is selected
	Description string `yaml:"description,omitempty"` // Semantic description of when to select this option
}

// DecisionLevel defines a single ordinal rubric level for a score question.
type DecisionLevel struct {
	Label       string `yaml:"label"`                 // Label for this level (e.g., "low", "medium", "high")
	Description string `yaml:"description,omitempty"` // Rubric description for this score level
}
