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
	Type         string          `yaml:"type"`                   // e.g., "ollama", "openai", "anthropic"
	Endpoint     string          `yaml:"endpoint"`               // API base URL
	DefaultModel string          `yaml:"defaultModel"`           // Model to use if none specified
	Auth         *ProviderAuth   `yaml:"auth,omitempty"`         // Authentication configuration
	Models       []ProviderModel `yaml:"models"`                 // Available models from this provider
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
		copy(out.Models, in.Models)
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
	ID     string              `yaml:"id"`     // Unique model ID (e.g., "gpt-4")
	Name   string              `yaml:"name"`   // Model name (e.g., "gpt-4")
	Label  string              `yaml:"label"`  // Human-friendly label (e.g., "GPT-4")
	Limits ProviderModelLimits `yaml:"limits"` // Context and output token limits
}

// ProviderModelLimits defines the token limits for a specific model.
type ProviderModelLimits struct {
	Context int `yaml:"context"` // Max context length in tokens
	Output  int `yaml:"output"`  // Max output length in tokens
}
