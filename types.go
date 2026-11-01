package warp

import (
	"fmt"
	"maps"
	"path/filepath"
)

const (
	// APIVersion is the expected apiVersion value in all resource front-matter.
	APIVersion = "warp/v1alpha1"
)

// Kind identifies the resource type declared in a file's front-matter.
type Kind string

const (
	// KindWorkspace represents a Workspace resource (WORKSPACE.md).
	KindWorkspace Kind = "Workspace"
	// KindContext represents a Context resource (AGENTS.md).
	KindContext Kind = "Context"
	// KindAgent represents an Agent resource.
	KindAgent Kind = "Agent"
	// KindSkill represents a Skill resource.
	KindSkill Kind = "Skill"
	// KindCommand represents a Command resource.
	KindCommand Kind = "Command"
	// KindDecision represents a Decision resource.
	KindDecision Kind = "Decision"
	// KindModelProvider represents an LLM Provider resource.
	KindModelProvider Kind = "ModelProvider"
	// KindTool represents a Custom Tool resource.
	KindTool Kind = "Tool"
	// KindMCP represents an MCP Server resource.
	KindMCP Kind = "MCP"
	// KindPlugin represents a Plugin resource.
	KindPlugin Kind = "Plugin"
	// KindRule represents a Rule resource.
	KindRule Kind = "Rule"
)

// Metadata holds the identifying and descriptive fields shared by every
// resource kind. These fields map directly to the front-matter "metadata"
// block in a Markdown file.
type Metadata struct {
	// Name is the unique identifier for the resource within its kind.
	Name string `yaml:"name"`
	// Description is a short human-readable summary of the resource.
	Description string `yaml:"description,omitempty"`
	// DisplayName is an optional pretty-printed label for UIs.
	DisplayName string `yaml:"displayName,omitempty"`
	// Labels are arbitrary key-value pairs for categorisation and filtering.
	Labels map[string]string `yaml:"labels,omitempty"`
}

// DeepCopy returns a deep copy of the Metadata.
func (in *Metadata) DeepCopy() *Metadata {
	if in == nil {
		return nil
	}
	out := new(Metadata)
	*out = *in
	if in.Labels != nil {
		out.Labels = make(map[string]string, len(in.Labels))
		maps.Copy(out.Labels, in.Labels)
	}
	return out
}

// ResourceFilter defines inclusion and exclusion rules based on glob patterns.
type ResourceFilter struct {
	Include []string `yaml:"include,omitempty"` // Glob patterns for resources to expose
	Exclude []string `yaml:"exclude,omitempty"` // Glob patterns for resources to block (applied after include)
}

// DeepCopy returns a deep copy of the ResourceFilter.
func (in *ResourceFilter) DeepCopy() *ResourceFilter {
	if in == nil {
		return nil
	}
	out := new(ResourceFilter)
	if in.Include != nil {
		out.Include = make([]string, len(in.Include))
		copy(out.Include, in.Include)
	}
	if in.Exclude != nil {
		out.Exclude = make([]string, len(in.Exclude))
		copy(out.Exclude, in.Exclude)
	}
	return out
}

// Argument defines a structured parameter accepted by a resource.
type Argument struct {
	// Name is the identifier for the argument, used in templating (e.g. $name).
	Name string `yaml:"name" json:"name"`
	// Description provides human and LLM guidance on the purpose of this argument.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Required indicates whether the argument must be supplied when invoked.
	Required bool `yaml:"required,omitempty" json:"required,omitempty"`
	// Default is the fallback value used when the argument is not provided.
	Default string `yaml:"default,omitempty" json:"default,omitempty"`
}

// DeepCopy returns a deep copy of the Argument.
func (in *Argument) DeepCopy() *Argument {
	if in == nil {
		return nil
	}
	out := new(Argument)
	*out = *in
	return out
}

// BaseResource contains the fields that every resource kind shares.
// Embed it with `yaml:",inline"` so the top-level YAML keys are promoted
// into the enclosing struct.
type BaseResource struct {
	// APIVersion declares the schema version, e.g. "tasksmith/v1".
	APIVersion string `yaml:"apiVersion"`
	// Kind identifies the resource type.
	Kind Kind `yaml:"kind"`
	// Metadata holds the resource's name, description, and display name.
	Metadata Metadata `yaml:"metadata"`
	// Directory is the FS path of the directory that contains the resource
	// file. It is populated by the Loader and is never serialised to YAML.
	Directory string `yaml:"-"`
	// Path is the full FS path of the resource file. It is populated by the
	// Loader and is never serialised to YAML or JSON.
	Path string `yaml:"-" json:"-"`
	// Namespace is the registry namespace this resource belongs to.
	// Set by the assembler; empty for resources loaded via the legacy
	// filesystem path.
	Namespace string `yaml:"-"`
}

// GetKind implements Resource.
func (b *BaseResource) GetKind() Kind { return b.Kind }

// GetName implements Resource.
func (b *BaseResource) GetName() string { return b.Metadata.Name }

// GetMetadata implements Resource.
func (b *BaseResource) GetMetadata() Metadata { return b.Metadata }

// GetNamespace implements Resource.
func (b *BaseResource) GetNamespace() string { return b.Namespace }

// GetDir implements Resource.
func (b *BaseResource) GetDir() string {
	if b == nil {
		return ""
	}
	return b.Directory
}

// GetPath implements Resource.
func (b *BaseResource) GetPath() string {
	if b == nil {
		return ""
	}
	if b.Path != "" {
		return b.Path
	}
	if b.Directory != "" && b.Metadata.Name != "" {
		return filepath.Join(b.Directory, b.Metadata.Name+".md")
	}
	return ""
}

// SetNamespace sets the namespace of this resource.
func (b *BaseResource) SetNamespace(ns string) { b.Namespace = ns }

// QualifiedName implements Resource.
func (b *BaseResource) QualifiedName() string {
	return MakeQualifiedName(b.Namespace, b.Kind, b.Metadata.Name)
}

// DeepCopy returns a deep copy of the BaseResource.
func (b *BaseResource) DeepCopy() *BaseResource {
	if b == nil {
		return nil
	}
	out := new(BaseResource)
	*out = *b
	out.Metadata = *b.Metadata.DeepCopy()
	return out
}

// ValidateBase verifies that the mandatory front-matter fields are present
// and that Kind is a known value. It returns a descriptive error for the
// first failing check it encounters.
func (b *BaseResource) ValidateBase() error {
	if b.APIVersion == "" {
		return fmt.Errorf("apiVersion is required")
	}
	if b.Kind == "" {
		return fmt.Errorf("kind is required")
	}
	switch b.Kind {
	case KindWorkspace, KindContext, KindAgent, KindSkill, KindCommand, KindDecision,
		KindModelProvider, KindTool, KindMCP, KindPlugin, KindRule:
		// valid
	default:
		return fmt.Errorf("unknown kind %q: must be one of Workspace, Context, Agent, Skill, Command, Decision, ModelProvider, Tool, MCP, Plugin, Rule", b.Kind)
	}
	if b.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	return nil
}

// ModelRole identifies the capability role for model resolution.
type ModelRole string

const (
	// ModelRoleCompletion is the role for text/chat completion models.
	ModelRoleCompletion ModelRole = "completion"
	// ModelRoleDecision is the role for probabilistic decision evaluation models.
	ModelRoleDecision ModelRole = "decision"
	// ModelRoleEmbedding is the role for vector embedding generation models.
	ModelRoleEmbedding ModelRole = "embedding"
)

// ModelDefaults defines role-based default models or providers for completion,
// decision evaluation, and embedding generation.
type ModelDefaults struct {
	Completion string `yaml:"completion,omitempty" json:"completion,omitempty"`
	Decision   string `yaml:"decision,omitempty" json:"decision,omitempty"`
	Embedding  string `yaml:"embedding,omitempty" json:"embedding,omitempty"`
}

// ForRole returns the default model configured for the given role, or "" if unset.
func (in *ModelDefaults) ForRole(role ModelRole) string {
	if in == nil {
		return ""
	}
	switch role {
	case ModelRoleCompletion:
		return in.Completion
	case ModelRoleDecision:
		return in.Decision
	case ModelRoleEmbedding:
		return in.Embedding
	default:
		return ""
	}
}

// DeepCopy returns a deep copy of the ModelDefaults.
func (in *ModelDefaults) DeepCopy() *ModelDefaults {
	if in == nil {
		return nil
	}
	out := new(ModelDefaults)
	*out = *in
	return out
}

// Policies defines security and access policies.
type Policies struct {
	Tools *ToolPolicies `yaml:"tools,omitempty"`
}

// DeepCopy returns a deep copy of the Policies.
func (in *Policies) DeepCopy() *Policies {
	if in == nil {
		return nil
	}
	out := new(Policies)
	out.Tools = in.Tools.DeepCopy()
	return out
}

// ToolPolicyAction defines the action to take for a tool evaluation.
type ToolPolicyAction string

const (
	ToolPolicyAllow ToolPolicyAction = "allow"
	ToolPolicyAsk   ToolPolicyAction = "ask"
	ToolPolicyDeny  ToolPolicyAction = "deny"
)

// ToolPolicies defines restrictions for tool usage.
type ToolPolicies struct {
	Dangerous ToolPolicyAction `yaml:"dangerous,omitempty" json:"dangerous,omitempty"`
	OpenWorld ToolPolicyAction `yaml:"openWorld,omitempty" json:"openWorld,omitempty"`
	ReadOnly  bool             `yaml:"readOnly,omitempty" json:"readOnly,omitempty"`
	Include   []string         `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude   []string         `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	Ask       []string         `yaml:"ask,omitempty" json:"ask,omitempty"`
}

// DeepCopy returns a deep copy of the ToolPolicies.
func (in *ToolPolicies) DeepCopy() *ToolPolicies {
	if in == nil {
		return nil
	}
	out := new(ToolPolicies)
	*out = *in
	if in.Include != nil {
		out.Include = make([]string, len(in.Include))
		copy(out.Include, in.Include)
	}
	if in.Exclude != nil {
		out.Exclude = make([]string, len(in.Exclude))
		copy(out.Exclude, in.Exclude)
	}
	if in.Ask != nil {
		out.Ask = make([]string, len(in.Ask))
		copy(out.Ask, in.Ask)
	}
	return out
}

// EvaluateTool evaluates a single tool against this policy and returns the resulting action.
func (p *ToolPolicies) EvaluateTool(tool *Tool) ToolPolicyAction {
	if tool == nil {
		return ToolPolicyDeny
	}
	isDangerous := tool.Spec.Annotations != nil && tool.Spec.Annotations.IsDangerous
	isOpenWorld := tool.Spec.Annotations != nil && tool.Spec.Annotations.IsOpenWorld

	if p == nil {
		if isDangerous {
			return ToolPolicyAsk
		}
		return ToolPolicyAllow
	}

	dangerous := p.Dangerous
	if dangerous == "" {
		dangerous = ToolPolicyAsk
	}
	openWorld := p.OpenWorld
	if openWorld == "" {
		openWorld = ToolPolicyAllow
	}

	if p.ReadOnly && (tool.Spec.Annotations == nil || !tool.Spec.Annotations.IsReadOnly) {
		return ToolPolicyDeny
	}

	shortName := tool.Metadata.Name
	nsName := tool.GetNamespace() + "/" + shortName
	qualifiedName := tool.QualifiedName()

	if len(p.Include) > 0 {
		matched := false
		for _, pat := range p.Include {
			if matchResourcePattern(pat, shortName, nsName, qualifiedName) {
				matched = true
				break
			}
		}
		if !matched {
			return ToolPolicyDeny
		}
	}

	for _, pat := range p.Exclude {
		if matchResourcePattern(pat, shortName, nsName, qualifiedName) {
			return ToolPolicyDeny
		}
	}

	if isDangerous && dangerous == ToolPolicyDeny {
		return ToolPolicyDeny
	}
	if isOpenWorld && openWorld == ToolPolicyDeny {
		return ToolPolicyDeny
	}

	for _, pat := range p.Ask {
		if matchResourcePattern(pat, shortName, nsName, qualifiedName) {
			return ToolPolicyAsk
		}
	}
	if isDangerous && dangerous == ToolPolicyAsk {
		return ToolPolicyAsk
	}
	if isOpenWorld && openWorld == ToolPolicyAsk {
		return ToolPolicyAsk
	}

	return ToolPolicyAllow
}

// ResolvedAgent represents a fully hydrated agent, with all inheritance merged
// and capabilities resolved and filtered by policies.
type ResolvedAgent struct {
	Agent           *Agent
	Tools           []*Tool
	Skills          []Skill
	Commands        []*Command
	Subagents       []*Agent
	Rules           []Rule
	WorkspacePolicy *ToolPolicies
}

// StricterToolPolicyAction returns the stricter of two tool policy actions
// based on the hierarchy: allow < ask < deny.
func StricterToolPolicyAction(a, b ToolPolicyAction) ToolPolicyAction {
	if a == ToolPolicyDeny || b == ToolPolicyDeny {
		return ToolPolicyDeny
	}
	if a == ToolPolicyAsk || b == ToolPolicyAsk {
		return ToolPolicyAsk
	}
	return ToolPolicyAllow
}

// ToolAction returns the effective ToolPolicyAction for a tool under this resolved agent,
// evaluating both workspace and agent policies and returning the stricter action.
func (ra *ResolvedAgent) ToolAction(tool *Tool) ToolPolicyAction {
	if ra == nil || tool == nil {
		return ToolPolicyDeny
	}
	var wsAction ToolPolicyAction = ToolPolicyAllow
	if ra.WorkspacePolicy != nil {
		wsAction = ra.WorkspacePolicy.EvaluateTool(tool)
	}
	var agAction ToolPolicyAction
	if ra.Agent != nil && ra.Agent.Spec.Policies != nil && ra.Agent.Spec.Policies.Tools != nil {
		agAction = ra.Agent.Spec.Policies.Tools.EvaluateTool(tool)
	} else {
		var nilPolicy *ToolPolicies
		agAction = nilPolicy.EvaluateTool(tool)
	}
	return StricterToolPolicyAction(wsAction, agAction)
}

// ResolvedModel represents a resolved model reference paired with its owning
// ModelProvider and declared ProviderModel metadata (when present in spec.models).
type ResolvedModel struct {
	// Provider is the owning ModelProvider resource in the registry, or nil for
	// unregistered pass-through references.
	Provider *ModelProvider
	// Model is the matched model entry from Provider.Spec.Models, or nil when
	// the model is ad-hoc / not listed in spec.models.
	Model *ProviderModel
	// ModelID is the model identifier stripped of its provider prefix
	// (e.g., "ddalcu/Qwen3.8-27B-MLX-Serve-4bit").
	ModelID string
	// QualifiedID is the canonical "<provider>/<model-id>" identifier
	// (e.g., "mlx-serve/ddalcu/Qwen3.8-27B-MLX-Serve-4bit").
	QualifiedID string
}
