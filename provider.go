package warp

import "maps"

// ModelProvider is a warp resource that describes an LLM provider configuration.
type ModelProvider struct {
	BaseResource `yaml:",inline"`
	// Spec holds the provider-specific configuration.
	Spec ModelProviderSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the ModelProvider.
func (in *ModelProvider) DeepCopy() *ModelProvider {
	if in == nil {
		return nil
	}
	out := new(ModelProvider)
	out.BaseResource = *in.BaseResource.DeepCopy()
	out.Spec = *in.Spec.DeepCopy()
	return out
}

func (in *ModelProvider) matchesRef(ref string) bool {
	return in != nil && ref != "" && (in.Metadata.Name == ref || in.QualifiedName() == ref)
}

func (in *ModelProvider) findModel(ref string) *ProviderModel {
	if in == nil || ref == "" {
		return nil
	}
	for i := range in.Spec.Models {
		if m := &in.Spec.Models[i]; m.ID == ref || (m.Name != "" && m.Name == ref) {
			return m
		}
	}
	return nil
}

func (in *ModelProvider) defaultForRole(role ModelRole) string {
	if in == nil {
		return ""
	}
	if m := in.Spec.DefaultModels.ForRole(role); m != "" {
		return m
	}
	if role == ModelRoleCompletion {
		return in.Spec.DefaultModel
	}
	return ""
}

// ModelProviderSpec contains the configuration details for a ModelProvider resource.
type ModelProviderSpec struct {
	Type          string            `yaml:"type"`                                                   // e.g., "ollama", "openai", "anthropic", "typesafe"
	Endpoint      string            `yaml:"endpoint"`                                               // API base URL
	DefaultModel  string            `yaml:"defaultModel,omitempty"`                                 // Default completion model to use if none specified
	DefaultModels *ModelDefaults    `yaml:"defaultModels,omitempty" json:"defaultModels,omitempty"` // Role-based default models (completion, decision, embedding)
	Auth          *ProviderAuth     `yaml:"auth,omitempty" json:"auth,omitempty"`                   // Authentication configuration
	Headers       map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`             // Custom HTTP headers for all provider requests
	Config        map[string]any    `yaml:"config,omitempty" json:"config,omitempty"`               // Provider-specific settings and custom configuration parameters
	Models        []ProviderModel   `yaml:"models"`                                                 // Available models from this provider
}

// DeepCopy returns a deep copy of the ModelProviderSpec.
func (in *ModelProviderSpec) DeepCopy() *ModelProviderSpec {
	if in == nil {
		return nil
	}
	out := new(ModelProviderSpec)
	*out = *in
	if in.DefaultModels != nil {
		out.DefaultModels = in.DefaultModels.DeepCopy()
	}
	if in.Auth != nil {
		out.Auth = in.Auth.DeepCopy()
	}
	if in.Headers != nil {
		out.Headers = make(map[string]string, len(in.Headers))
		maps.Copy(out.Headers, in.Headers)
	}
	if in.Config != nil {
		out.Config = make(map[string]any, len(in.Config))
		maps.Copy(out.Config, in.Config)
	}
	if in.Models != nil {
		out.Models = make([]ProviderModel, len(in.Models))
		for i, m := range in.Models {
			out.Models[i] = *m.DeepCopy()
		}
	}
	return out
}

// ProviderAuth defines how to authenticate with the model provider.
type ProviderAuth struct {
	Type   string `yaml:"type,omitempty" json:"type,omitempty"`     // The auth scheme (e.g., "bearer", "api-key", "basic")
	Header string `yaml:"header,omitempty" json:"header,omitempty"` // Custom header name if type is "api-key"
	Env    string `yaml:"env,omitempty" json:"env,omitempty"`       // Read credential from environment variable
	File   string `yaml:"file,omitempty" json:"file,omitempty"`     // Read credential from file path
}

// DeepCopy returns a deep copy of the ProviderAuth.
func (in *ProviderAuth) DeepCopy() *ProviderAuth {
	if in == nil {
		return nil
	}
	out := new(ProviderAuth)
	*out = *in
	return out
}

// ProviderModel describes a specific model available from a provider.
type ProviderModel struct {
	ID           string                     `yaml:"id"`                     // Unique model ID (e.g., "gpt-4")
	Name         string                     `yaml:"name"`                   // Model name (e.g., "gpt-4")
	Label        string                     `yaml:"label"`                  // Human-friendly label (e.g., "GPT-4")
	Limits       ProviderModelLimits        `yaml:"limits"`                 // Context, output token, and vector dimension limits
	Capabilities *ProviderModelCapabilities `yaml:"capabilities,omitempty"` // Supported features and configuration boundaries
	Cost         *ProviderModelCost         `yaml:"cost,omitempty"`         // Token pricing and conditional tiers
}

func (in *ProviderModel) DeepCopy() *ProviderModel {
	if in == nil {
		return nil
	}
	out := new(ProviderModel)
	*out = *in
	if in.Capabilities != nil {
		out.Capabilities = in.Capabilities.DeepCopy()
	}
	if in.Cost != nil {
		out.Cost = in.Cost.DeepCopy()
	}
	return out
}

// ProviderModelLimits defines the token and vector dimension limits for a specific model.
type ProviderModelLimits struct {
	Context    int `yaml:"context"`              // Max context length in tokens
	Output     int `yaml:"output,omitempty"`     // Max output length in tokens
	Dimensions int `yaml:"dimensions,omitempty"` // Default/max output vector dimensions (for embedding models)
}

type ProviderModelCapabilities struct {
	Completion       *bool                             `yaml:"completion,omitempty"`       // Whether the model supports text/chat completion (defaults to true)
	Decision         *bool                             `yaml:"decision,omitempty"`         // Whether the model supports probabilistic decision evaluation (defaults to false)
	Embedding        *bool                             `yaml:"embedding,omitempty"`        // Whether the model supports vector embedding generation (defaults to false)
	Dimensions       []int                             `yaml:"dimensions,omitempty,flow"`  // Supported output vector dimensions for configurable embedding models
	Attachment       *bool                             `yaml:"attachment,omitempty"`       // Whether the model supports file/document attachments
	Tools            *bool                             `yaml:"tools,omitempty"`            // Whether the model supports function/tool calling (tool_call)
	StructuredOutput *bool                             `yaml:"structuredOutput,omitempty"` // Whether the model supports strict structured outputs
	Temperature      *bool                             `yaml:"temperature,omitempty"`      // Whether the model allows configuring the sampling temperature
	Thinking         []ProviderModelThinkingCapability `yaml:"thinking,omitempty"`         // List of supported reasoning configuration strategies
	Modalities       *ProviderModelModalities          `yaml:"modalities,omitempty"`       // Defines supported input and output data modalities
}

// SupportsCompletion reports whether the model supports text/chat completions.
// Defaults to true when Capabilities or Completion is nil.
func (in *ProviderModelCapabilities) SupportsCompletion() bool {
	if in == nil || in.Completion == nil {
		return true
	}
	return *in.Completion
}

// SupportsDecision reports whether the model supports probabilistic decision evaluation.
// Defaults to false when Capabilities or Decision is nil.
func (in *ProviderModelCapabilities) SupportsDecision() bool {
	if in == nil || in.Decision == nil {
		return false
	}
	return *in.Decision
}

// SupportsEmbedding reports whether the model supports vector embedding generation.
// Defaults to false when Capabilities or Embedding is nil.
func (in *ProviderModelCapabilities) SupportsEmbedding() bool {
	if in == nil || in.Embedding == nil {
		return false
	}
	return *in.Embedding
}

func (in *ProviderModelCapabilities) supportsRole(role ModelRole) bool {
	switch role {
	case ModelRoleCompletion:
		return in.SupportsCompletion()
	case ModelRoleDecision:
		return in.SupportsDecision()
	case ModelRoleEmbedding:
		return in.SupportsEmbedding()
	default:
		return false
	}
}

func (in *ProviderModelCapabilities) DeepCopy() *ProviderModelCapabilities {
	if in == nil {
		return nil
	}
	out := new(ProviderModelCapabilities)
	*out = *in
	if in.Completion != nil {
		b := *in.Completion
		out.Completion = &b
	}
	if in.Decision != nil {
		b := *in.Decision
		out.Decision = &b
	}
	if in.Embedding != nil {
		b := *in.Embedding
		out.Embedding = &b
	}
	if in.Dimensions != nil {
		out.Dimensions = make([]int, len(in.Dimensions))
		copy(out.Dimensions, in.Dimensions)
	}
	if in.Attachment != nil {
		b := *in.Attachment
		out.Attachment = &b
	}
	if in.Tools != nil {
		b := *in.Tools
		out.Tools = &b
	}
	if in.StructuredOutput != nil {
		b := *in.StructuredOutput
		out.StructuredOutput = &b
	}
	if in.Temperature != nil {
		b := *in.Temperature
		out.Temperature = &b
	}
	if in.Thinking != nil {
		out.Thinking = make([]ProviderModelThinkingCapability, len(in.Thinking))
		for i, t := range in.Thinking {
			out.Thinking[i] = *t.DeepCopy()
		}
	}
	if in.Modalities != nil {
		out.Modalities = in.Modalities.DeepCopy()
	}
	return out
}

type ProviderModelThinkingCapability struct {
	Type            string   `yaml:"type"`                      // The strategy this model supports: toggle, effort, budget, or adaptive
	IsDefault       bool     `yaml:"isDefault,omitempty"`       // Marks this strategy as the primary default
	AllowedEfforts  []string `yaml:"allowedEfforts,omitempty"`  // Used if type: effort. e.g., ["low", "medium", "high"]
	MaxBudgetTokens int      `yaml:"maxBudgetTokens,omitempty"` // Used if type: budget. The maximum allowed thinking tokens
	Default         any      `yaml:"default,omitempty"`         // The default value
}

func (in *ProviderModelThinkingCapability) DeepCopy() *ProviderModelThinkingCapability {
	if in == nil {
		return nil
	}
	out := new(ProviderModelThinkingCapability)
	*out = *in
	if in.AllowedEfforts != nil {
		out.AllowedEfforts = make([]string, len(in.AllowedEfforts))
		copy(out.AllowedEfforts, in.AllowedEfforts)
	}
	return out
}

type ProviderModelModalities struct {
	Input  []string `yaml:"input,omitempty"`  // Supported input modalities (e.g., ["text", "image", "video"])
	Output []string `yaml:"output,omitempty"` // Supported output modalities (e.g., ["text", "image"])
}

func (in *ProviderModelModalities) DeepCopy() *ProviderModelModalities {
	if in == nil {
		return nil
	}
	out := new(ProviderModelModalities)
	*out = *in
	if in.Input != nil {
		out.Input = make([]string, len(in.Input))
		copy(out.Input, in.Input)
	}
	if in.Output != nil {
		out.Output = make([]string, len(in.Output))
		copy(out.Output, in.Output)
	}
	return out
}

type ProviderModelCost struct {
	Input      float64                 `yaml:"input"`                // Base cost for input tokens (typically per 1M tokens)
	Output     float64                 `yaml:"output,omitempty"`     // Base cost for output tokens
	CacheRead  float64                 `yaml:"cacheRead,omitempty"`  // Cost for reading from prompt cache
	CacheWrite float64                 `yaml:"cacheWrite,omitempty"` // Cost for writing to prompt cache
	Tiers      []ProviderModelCostTier `yaml:"tiers,omitempty"`      // Array of conditional pricing tiers
}

func (in *ProviderModelCost) DeepCopy() *ProviderModelCost {
	if in == nil {
		return nil
	}
	out := new(ProviderModelCost)
	*out = *in
	if in.Tiers != nil {
		out.Tiers = make([]ProviderModelCostTier, len(in.Tiers))
		for i, t := range in.Tiers {
			out.Tiers[i] = *t.DeepCopy()
		}
	}
	return out
}

type ProviderModelCostTier struct {
	Label      string                         `yaml:"label"`                // A human-readable label for the tier (e.g., "Context over 200k")
	Tier       ProviderModelCostTierCondition `yaml:"tier"`                 // The condition that triggers this pricing
	Input      float64                        `yaml:"input"`                // Override cost for input tokens
	Output     float64                        `yaml:"output,omitempty"`     // Override cost for output tokens
	CacheRead  float64                        `yaml:"cacheRead,omitempty"`  // Override cost for cache read
	CacheWrite float64                        `yaml:"cacheWrite,omitempty"` // Override cost for cache write
}

func (in *ProviderModelCostTier) DeepCopy() *ProviderModelCostTier {
	if in == nil {
		return nil
	}
	out := new(ProviderModelCostTier)
	*out = *in
	return out
}

type ProviderModelCostTierCondition struct {
	Type string `yaml:"type"` // e.g., "context"
	Size int    `yaml:"size"` // e.g., 200000
}
