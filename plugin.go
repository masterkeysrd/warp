package warp

// Plugin is a warp resource that declares a repository as a distributable package.
type Plugin struct {
	BaseResource `yaml:",inline"`
	// Spec holds the Plugin-specific configuration.
	Spec PluginSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Plugin.
func (in *Plugin) DeepCopy() *Plugin {
	if in == nil {
		return nil
	}
	out := new(Plugin)
	out.BaseResource = *in.BaseResource.DeepCopy()
	out.Spec = *in.Spec.DeepCopy()
	return out
}

// PluginSpec contains the configuration details for a Plugin resource.
type PluginSpec struct {
	ResourceDir string   `yaml:"resourceDir"` // The relative path within the repository where the loader should look for resources
	Exports     []string `yaml:"exports"`     // Glob patterns defining which resources are exposed to consumers
}

// DeepCopy returns a deep copy of the PluginSpec.
func (in *PluginSpec) DeepCopy() *PluginSpec {
	if in == nil {
		return nil
	}
	out := new(PluginSpec)
	*out = *in
	if in.Exports != nil {
		out.Exports = make([]string, len(in.Exports))
		copy(out.Exports, in.Exports)
	}
	return out
}
