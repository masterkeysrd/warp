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
	// Instructions is the Markdown body of the plugin file.
	Instructions string       `yaml:"instructions,omitempty"`
	ResourceDir  string       `yaml:"resourceDir"`     // The relative path within the repository where the loader should look for resources
	Exports      []string     `yaml:"exports"`         // Glob patterns defining which resources are exposed to consumers
	Hooks        *PluginHooks `yaml:"hooks,omitempty"` // Setup and installation hooks
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
	if in.Hooks != nil {
		out.Hooks = in.Hooks.DeepCopy()
	}
	return out
}

// PluginHooks defines lifecycle commands to run when a plugin is installed.
type PluginHooks struct {
	PostInstall [][]string `yaml:"postInstall,omitempty"` // Commands to run after the plugin is downloaded (e.g., ["go", "install", "./..."])
}

// DeepCopy returns a deep copy of the PluginHooks.
func (in *PluginHooks) DeepCopy() *PluginHooks {
	if in == nil {
		return nil
	}
	out := new(PluginHooks)
	if in.PostInstall != nil {
		out.PostInstall = make([][]string, len(in.PostInstall))
		for i, v := range in.PostInstall {
			out.PostInstall[i] = make([]string, len(v))
			copy(out.PostInstall[i], v)
		}
	}
	return out
}
