package warp

// FileSystemPersistence declares how long the data of a FileSystem lives.
type FileSystemPersistence string

const (
	// FileSystemPersistenceEphemeral marks data that exists only for the
	// lifetime of one sandbox instance.
	FileSystemPersistenceEphemeral FileSystemPersistence = "ephemeral"
	// FileSystemPersistencePersistent marks data that outlives the sandbox.
	FileSystemPersistencePersistent FileSystemPersistence = "persistent"
)

// Reserved filesystem driver names. Custom drivers must use a
// domain-qualified name whose first segment contains a dot (e.g.
// "acme.com/s3fs").
const (
	// FileSystemDriverHost is a physical directory on the host (config.path).
	FileSystemDriverHost = "host"
	// FileSystemDriverOverlay is a copy-on-write layer over another
	// FileSystem (config.source).
	FileSystemDriverOverlay = "overlay"
	// FileSystemDriverMemory is an in-memory scratch filesystem (config.size).
	FileSystemDriverMemory = "memory"
	// FileSystemDriverGit is a checkout of a repository at a ref
	// (config.url, config.ref, config.depth).
	FileSystemDriverGit = "git"
)

// Reserved filesystem driver capability names.
const (
	// FileSystemCapabilityWrite reports that the driver supports writes.
	FileSystemCapabilityWrite = "write"
	// FileSystemCapabilityRename reports that the driver supports atomic rename.
	FileSystemCapabilityRename = "rename"
	// FileSystemCapabilityAppend reports that the driver supports appends.
	FileSystemCapabilityAppend = "append"
)

// FileSystemCwd is the qualified name of the runtime-provided host
// filesystem whose path is the runtime's working directory.
const FileSystemCwd = "system/FileSystem/cwd"

// ReservedFileSystemDrivers lists the unqualified filesystem driver names
// reserved by the specification.
var ReservedFileSystemDrivers = []string{
	FileSystemDriverHost,
	FileSystemDriverOverlay,
	FileSystemDriverMemory,
	FileSystemDriverGit,
}

// FileSystem is a warp resource that describes data a Sandbox can mount: a
// physical directory, a virtual filesystem, or a remote store. The filesystem
// declares what the data is; the mounting Sandbox declares where it appears
// and with what access.
type FileSystem struct {
	BaseResource `yaml:",inline"`
	// Spec holds the filesystem-specific configuration.
	Spec FileSystemSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the FileSystem.
func (in *FileSystem) DeepCopy() *FileSystem {
	if in == nil {
		return nil
	}
	out := new(FileSystem)
	out.BaseResource = *in.BaseResource.DeepCopy()
	out.Spec = *in.Spec.DeepCopy()
	return out
}

// FileSystemSpec contains the configuration details for a FileSystem resource.
type FileSystemSpec struct {
	Enabled     *bool                 `yaml:"enabled,omitempty" json:"enabled,omitempty"`         // Whether this filesystem is active (defaults to true when omitted)
	Driver      string                `yaml:"driver" json:"driver"`                               // Reserved driver (host, overlay, memory, git) or a domain-qualified custom driver
	Config      map[string]any        `yaml:"config,omitempty" json:"config,omitempty"`           // Driver-specific settings; opaque to WARP and templated (e.g. $ProjectDir)
	Auth        *ProviderAuth         `yaml:"auth,omitempty" json:"auth,omitempty"`               // Credentials for remote backends; never exposed inside the sandbox
	Persistence FileSystemPersistence `yaml:"persistence,omitempty" json:"persistence,omitempty"` // ephemeral or persistent (driver default when omitted)
	ReadOnly    bool                  `yaml:"readOnly,omitempty" json:"readOnly,omitempty"`       // Forces every mount of this filesystem to ro
}

// IsEnabled reports whether this FileSystem is enabled (true when Enabled is nil or *Enabled is true).
func (in *FileSystemSpec) IsEnabled() bool {
	return in == nil || in.Enabled == nil || *in.Enabled
}

// DeepCopy returns a deep copy of the FileSystemSpec.
func (in *FileSystemSpec) DeepCopy() *FileSystemSpec {
	if in == nil {
		return nil
	}
	out := new(FileSystemSpec)
	*out = *in
	out.Enabled = copyBoolPtr(in.Enabled)
	out.Config = deepCopyConfig(in.Config)
	out.Auth = in.Auth.DeepCopy()
	return out
}

// ConfigString returns the string value of a config key, or "" when the key
// is absent or not a string.
func (in *FileSystemSpec) ConfigString(key string) string {
	if in == nil || in.Config == nil {
		return ""
	}
	s, _ := in.Config[key].(string)
	return s
}

// EffectivePersistence returns the declared persistence, or the reserved
// driver's default when omitted (host: persistent; overlay, memory, git:
// ephemeral). Custom drivers without a declared persistence return "".
func (in *FileSystemSpec) EffectivePersistence() FileSystemPersistence {
	if in == nil {
		return ""
	}
	if in.Persistence != "" {
		return in.Persistence
	}
	switch in.Driver {
	case FileSystemDriverHost:
		return FileSystemPersistencePersistent
	case FileSystemDriverOverlay, FileSystemDriverMemory, FileSystemDriverGit:
		return FileSystemPersistenceEphemeral
	}
	return ""
}

// deepCopyConfig returns a deep copy of an opaque driver configuration map,
// recursively copying nested maps and slices.
func deepCopyConfig(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = deepCopyConfigValue(v)
	}
	return out
}

func deepCopyConfigValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return deepCopyConfig(t)
	case []any:
		cp := make([]any, len(t))
		for i, e := range t {
			cp[i] = deepCopyConfigValue(e)
		}
		return cp
	case []string:
		cp := make([]string, len(t))
		copy(cp, t)
		return cp
	default:
		return v
	}
}
