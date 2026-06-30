package warp

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

// ModelProviderSpec contains the configuration details for a ModelProvider resource.
type ModelProviderSpec struct {
	Type         string          `yaml:"type"`           // e.g., "ollama", "openai", "anthropic"
	Endpoint     string          `yaml:"endpoint"`       // API base URL
	DefaultModel string          `yaml:"defaultModel"`   // Model to use if none specified
	Auth         *ProviderAuth   `yaml:"auth,omitempty"` // Authentication configuration
	Models       []ProviderModel `yaml:"models"`         // Available models from this provider
}

// DeepCopy returns a deep copy of the ModelProviderSpec.
func (in *ModelProviderSpec) DeepCopy() *ModelProviderSpec {
	if in == nil {
		return nil
	}
	out := new(ModelProviderSpec)
	*out = *in
	if in.Auth != nil {
		out.Auth = in.Auth.DeepCopy()
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
	Limits       ProviderModelLimits        `yaml:"limits"`                 // Context and output token limits
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

// ProviderModelLimits defines the token limits for a specific model.
type ProviderModelLimits struct {
	Context int `yaml:"context"` // Max context length in tokens
	Output  int `yaml:"output"`  // Max output length in tokens
}

type ProviderModelCapabilities struct {
	Attachment       *bool                             `yaml:"attachment,omitempty"`       // Whether the model supports file/document attachments
	Tools            *bool                             `yaml:"tools,omitempty"`            // Whether the model supports function/tool calling (tool_call)
	StructuredOutput *bool                             `yaml:"structuredOutput,omitempty"` // Whether the model supports strict structured outputs
	Temperature      *bool                             `yaml:"temperature,omitempty"`      // Whether the model allows configuring the sampling temperature
	Thinking         []ProviderModelThinkingCapability `yaml:"thinking,omitempty"`         // List of supported reasoning configuration strategies
	Modalities       *ProviderModelModalities          `yaml:"modalities,omitempty"`       // Defines supported input and output data modalities
}

func (in *ProviderModelCapabilities) DeepCopy() *ProviderModelCapabilities {
	if in == nil {
		return nil
	}
	out := new(ProviderModelCapabilities)
	*out = *in
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
	Output     float64                 `yaml:"output"`               // Base cost for output tokens
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
	Output     float64                        `yaml:"output"`               // Override cost for output tokens
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
