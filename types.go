package warp

import "fmt"

const (
	// APIVersion is the expected apiVersion value in all resource front-matter.
	APIVersion = "warp/v1alpha1"
)

// Kind identifies the resource type declared in a file's front-matter.
type Kind string

const (
	// KindWorkspace represents a Workspace resource (WORKSPACE.md).
	KindWorkspace Kind = "Workspace"
	// KindContext represents a Context resource (AGENT.md).
	KindContext Kind = "Context"
	// KindAgent represents an Agent resource.
	KindAgent Kind = "Agent"
	// KindSkill represents a Skill resource.
	KindSkill Kind = "Skill"
	// KindCommand represents a Command resource.
	KindCommand Kind = "Command"
	// KindModelProvider represents an LLM Provider resource.
	KindModelProvider Kind = "ModelProvider"
	// KindTool represents a Custom Tool resource.
	KindTool Kind = "Tool"
	// KindMCP represents an MCP Server resource.
	KindMCP Kind = "MCP"
	// KindToolkit represents a Toolkit resource.
	KindToolkit Kind = "Toolkit"
	// KindPlugin represents a Plugin resource.
	KindPlugin Kind = "Plugin"
)

// Metadata holds the identifying and descriptive fields shared by every
// resource kind. These fields map directly to the front-matter "metadata"
// block in a Markdown file.
type Metadata struct {
	// Name is the unique identifier for the resource within its kind.
	Name string `yaml:"name"`
	// Description is a short human-readable summary of the resource.
	Description string `yaml:"description"`
	// DisplayName is an optional pretty-printed label for UIs.
	DisplayName string `yaml:"displayName"`
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
	return out
}

// ResourceFilter defines inclusion and exclusion rules based on glob patterns.
type ResourceFilter struct {
	Include []string `yaml:"include"` // Glob patterns for resources to expose
	Exclude []string `yaml:"exclude"` // Glob patterns for resources to block (applied after include)
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
	case KindWorkspace, KindContext, KindAgent, KindSkill, KindCommand,
		KindModelProvider, KindTool, KindMCP, KindToolkit, KindPlugin:
		// valid
	default:
		return fmt.Errorf("unknown kind %q: must be one of Workspace, Context, Agent, Skill, Command, Plugin", b.Kind)
	}
	if b.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	return nil
}
