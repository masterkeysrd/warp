package warp

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// SandboxNone is the reserved value of Agent.spec.sandbox, Tool.spec.sandbox,
// and MCP.spec.sandbox that selects no sandbox (unsandboxed execution).
const SandboxNone = "none"

// SandboxDefault is the qualified name of the runtime-provided default
// sandbox used by selection step 4.
const SandboxDefault = "system/Sandbox/default"

// Reserved sandbox driver names. They describe an execution model, not a
// product. Custom drivers must use a domain-qualified name whose first segment
// contains a dot (e.g. "e2b.dev/sandbox").
const (
	// SandboxDriverLocal runs processes directly on the host without OS-level confinement.
	SandboxDriverLocal = "local"
	// SandboxDriverNative confines host processes with OS sandbox primitives.
	SandboxDriverNative = "native"
	// SandboxDriverContainer runs OCI containers.
	SandboxDriverContainer = "container"
	// SandboxDriverMicroVM runs lightweight virtual machines.
	SandboxDriverMicroVM = "microvm"
	// SandboxDriverVirtual is an in-memory runtime without real OS processes.
	SandboxDriverVirtual = "virtual"
)

// ReservedSandboxDrivers lists the unqualified sandbox driver names reserved
// by the specification.
var ReservedSandboxDrivers = []string{
	SandboxDriverLocal,
	SandboxDriverNative,
	SandboxDriverContainer,
	SandboxDriverMicroVM,
	SandboxDriverVirtual,
}

// Reserved sandbox capability names reported by drivers (see the Capability
// Vocabulary). Vendor-specific capabilities must use the "x-<vendor>/" prefix.
const (
	SandboxCapabilityExec             = "exec"
	SandboxCapabilityExecNative       = "exec.native"
	SandboxCapabilityExecAllowlist    = "exec.allowlist"
	SandboxCapabilityRootFSPrivate    = "rootfs.private"
	SandboxCapabilityRootFSReadOnly   = "rootfs.readonly"
	SandboxCapabilityRootFSNone       = "rootfs.none"
	SandboxCapabilityMounts           = "mounts"
	SandboxCapabilityMountsRemap      = "mounts.remap"
	SandboxCapabilityAccessRules      = "access.rules"
	SandboxCapabilityNetworkNone      = "network.none"
	SandboxCapabilityNetworkAllowlist = "network.allowlist"
	SandboxCapabilityEnvIsolation     = "env.isolation"
	SandboxCapabilityLimitsCPU        = "limits.cpu"
	SandboxCapabilityLimitsMemory     = "limits.memory"
	SandboxCapabilityLimitsProcesses  = "limits.processes"
	SandboxCapabilityLimitsTimeout    = "limits.timeout"
)

// DriverInfo describes a sandbox or filesystem driver implemented by a
// runtime. Runtimes pass driver metadata to validation through
// ValidateOptions.SandboxDrivers and ValidateOptions.FileSystemDrivers so that
// configuration and capabilities can be checked before execution.
type DriverInfo struct {
	// Capabilities lists the capabilities the driver (together with any
	// runtime-level enforcement layer) can enforce, e.g. "exec",
	// "mounts.remap", or "write".
	Capabilities []string
	// Commands optionally lists the commands offered by the driver (e.g. by a
	// virtual runtime). When non-empty, Tool.spec.command[0] must be one of them.
	Commands []string
	// ValidateConfig optionally validates the resource's spec.config.
	ValidateConfig func(config map[string]any) error
}

// HasCapability reports whether the driver reports the given capability.
func (d DriverInfo) HasCapability(capability string) bool {
	return slices.Contains(d.Capabilities, capability)
}

// MissingCapabilities returns the subset of required capabilities the driver
// does not report, preserving the order of required.
func (d DriverInfo) MissingCapabilities(required []string) []string {
	var missing []string
	for _, c := range required {
		if !d.HasCapability(c) {
			missing = append(missing, c)
		}
	}
	return missing
}

// IsQualifiedDriverName reports whether name is a domain-qualified custom
// driver name: it contains a "/" and its first segment contains a dot (e.g.
// "e2b.dev/sandbox").
func IsQualifiedDriverName(name string) bool {
	first, rest, ok := strings.Cut(name, "/")
	return ok && first != "" && rest != "" && strings.Contains(first, ".") &&
		!strings.HasPrefix(first, ".") && !strings.HasSuffix(first, ".")
}

// validateDriverName checks that name is either one of the reserved names or
// a domain-qualified custom driver name.
func validateDriverName(kindLabel, name string, reserved []string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("spec.driver is required")
	}
	if !strings.Contains(name, "/") {
		if slices.Contains(reserved, name) {
			return nil
		}
		return fmt.Errorf("unknown %s driver %q: unqualified drivers must be one of %s; custom drivers must be domain-qualified (e.g. example.com/driver)", kindLabel, name, strings.Join(reserved, ", "))
	}
	if !IsQualifiedDriverName(name) {
		return fmt.Errorf("invalid %s driver %q: custom drivers must be domain-qualified with a first segment containing a dot (e.g. example.com/driver)", kindLabel, name)
	}
	return nil
}

// ─── SandboxProvider ────────────────────────────────────────────────────────

// SandboxProvider is a warp resource that describes how isolated execution is
// provided: which driver implements it, where that driver is reached, and with
// what credentials.
type SandboxProvider struct {
	BaseResource `yaml:",inline"`
	// Spec holds the sandbox provider configuration.
	Spec SandboxProviderSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the SandboxProvider.
func (in *SandboxProvider) DeepCopy() *SandboxProvider {
	if in == nil {
		return nil
	}
	out := new(SandboxProvider)
	out.BaseResource = *in.BaseResource.DeepCopy()
	out.Spec = *in.Spec.DeepCopy()
	return out
}

// SandboxProviderSpec contains the configuration details for a SandboxProvider resource.
type SandboxProviderSpec struct {
	Enabled  *bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`   // Whether this provider is active (defaults to true when omitted)
	Driver   string         `yaml:"driver" json:"driver"`                         // Reserved driver (local, native, container, microvm, virtual) or a domain-qualified custom driver
	Endpoint string         `yaml:"endpoint,omitempty" json:"endpoint,omitempty"` // Driver endpoint (e.g. unix:///var/run/docker.sock); supports WARP templating
	Auth     *ProviderAuth  `yaml:"auth,omitempty" json:"auth,omitempty"`         // Credentials used only by the driver
	Config   map[string]any `yaml:"config,omitempty" json:"config,omitempty"`     // Driver-specific settings; opaque to WARP
}

// IsEnabled reports whether this SandboxProvider is enabled (true when Enabled is nil or *Enabled is true).
func (in *SandboxProviderSpec) IsEnabled() bool {
	return in == nil || in.Enabled == nil || *in.Enabled
}

// DeepCopy returns a deep copy of the SandboxProviderSpec.
func (in *SandboxProviderSpec) DeepCopy() *SandboxProviderSpec {
	if in == nil {
		return nil
	}
	out := new(SandboxProviderSpec)
	*out = *in
	out.Enabled = copyBoolPtr(in.Enabled)
	out.Auth = in.Auth.DeepCopy()
	out.Config = deepCopyConfig(in.Config)
	return out
}

// ─── Sandbox ────────────────────────────────────────────────────────────────

// Sandbox is a warp resource that describes what an execution environment may
// access: root filesystem, mounts, working directory, path access rules,
// command execution, network, environment, and limits.
type Sandbox struct {
	BaseResource `yaml:",inline"`
	// Spec holds the sandbox configuration.
	Spec SandboxSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Sandbox.
func (in *Sandbox) DeepCopy() *Sandbox {
	if in == nil {
		return nil
	}
	out := new(Sandbox)
	out.BaseResource = *in.BaseResource.DeepCopy()
	out.Spec = *in.Spec.DeepCopy()
	return out
}

// SandboxSpec contains the configuration details for a Sandbox resource.
type SandboxSpec struct {
	Enabled  *bool           `yaml:"enabled,omitempty" json:"enabled,omitempty"`   // Whether this sandbox is active (defaults to true when omitted)
	Extends  string          `yaml:"extends,omitempty" json:"extends,omitempty"`   // Reference to another Sandbox to inherit from
	Provider string          `yaml:"provider,omitempty" json:"provider,omitempty"` // Reference to a SandboxProvider (inherited, else the runtime's default provider)
	Config   map[string]any  `yaml:"config,omitempty" json:"config,omitempty"`     // Per-sandbox driver settings, shallow-merged over the provider's config
	RootFS   *SandboxRootFS  `yaml:"rootfs,omitempty" json:"rootfs,omitempty"`     // Root filesystem settings
	Mounts   []SandboxMount  `yaml:"mounts,omitempty" json:"mounts,omitempty"`     // Filesystems attached to the sandbox
	Workdir  string          `yaml:"workdir,omitempty" json:"workdir,omitempty"`   // Absolute path where execution starts
	Access   *SandboxAccess  `yaml:"access,omitempty" json:"access,omitempty"`     // Per-path read/write rules
	Exec     *SandboxExec    `yaml:"exec,omitempty" json:"exec,omitempty"`         // Command execution rules (default mode: any)
	Network  *SandboxNetwork `yaml:"network,omitempty" json:"network,omitempty"`   // Network egress rules (default mode: none)
	Env      *SandboxEnv     `yaml:"env,omitempty" json:"env,omitempty"`           // Environment exposure
	Limits   *SandboxLimits  `yaml:"limits,omitempty" json:"limits,omitempty"`     // Resource limits
}

// IsEnabled reports whether this Sandbox is enabled (true when Enabled is nil or *Enabled is true).
func (in *SandboxSpec) IsEnabled() bool {
	return in == nil || in.Enabled == nil || *in.Enabled
}

// DeepCopy returns a deep copy of the SandboxSpec.
func (in *SandboxSpec) DeepCopy() *SandboxSpec {
	if in == nil {
		return nil
	}
	out := new(SandboxSpec)
	*out = *in
	out.Enabled = copyBoolPtr(in.Enabled)
	out.Config = deepCopyConfig(in.Config)
	out.RootFS = in.RootFS.DeepCopy()
	if in.Mounts != nil {
		out.Mounts = make([]SandboxMount, len(in.Mounts))
		copy(out.Mounts, in.Mounts)
	}
	out.Access = in.Access.DeepCopy()
	out.Exec = in.Exec.DeepCopy()
	out.Network = in.Network.DeepCopy()
	out.Env = in.Env.DeepCopy()
	out.Limits = in.Limits.DeepCopy()
	return out
}

// SandboxRootFSMode is the mode of a sandbox root filesystem.
type SandboxRootFSMode string

const (
	// SandboxRootFSNone means no root filesystem: "/" is an empty, read-only
	// directory containing only mount points.
	SandboxRootFSNone SandboxRootFSMode = "none"
	// SandboxRootFSReadOnly is a read-only root filesystem.
	SandboxRootFSReadOnly SandboxRootFSMode = "ro"
	// SandboxRootFSReadWrite is a private, writable root filesystem.
	SandboxRootFSReadWrite SandboxRootFSMode = "rw"
)

// SandboxRootFS configures the root filesystem of a sandbox.
type SandboxRootFS struct {
	// Mode is rw, ro, or none. Defaults to rw when the driver supports
	// rootfs.private, otherwise ro.
	Mode SandboxRootFSMode `yaml:"mode,omitempty" json:"mode,omitempty"`
}

// DeepCopy returns a deep copy of the SandboxRootFS.
func (in *SandboxRootFS) DeepCopy() *SandboxRootFS {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// SandboxMountMode is the access mode of a sandbox mount.
type SandboxMountMode string

const (
	// SandboxMountReadOnly mounts a filesystem read-only (the default).
	SandboxMountReadOnly SandboxMountMode = "ro"
	// SandboxMountReadWrite mounts a filesystem read-write.
	SandboxMountReadWrite SandboxMountMode = "rw"
)

// SandboxMount attaches a FileSystem to a sandbox.
type SandboxMount struct {
	FileSystem string           `yaml:"filesystem" json:"filesystem"`         // Reference to a FileSystem
	Path       string           `yaml:"path,omitempty" json:"path,omitempty"` // Absolute mount target (defaults to the host source path for host-backed filesystems)
	Mode       SandboxMountMode `yaml:"mode,omitempty" json:"mode,omitempty"` // ro (default) or rw
}

// SandboxAccess declares per-path read/write rules. Absolute globs match
// sandbox paths; relative globs resolve against the sandbox workdir.
type SandboxAccess struct {
	Read  []string `yaml:"read,omitempty" json:"read,omitempty"`   // When set, reads are limited to matching paths (plus paths matching write)
	Write []string `yaml:"write,omitempty" json:"write,omitempty"` // When set, writes are limited to matching paths
	Deny  []string `yaml:"deny,omitempty" json:"deny,omitempty"`   // Paths that may never be read or written
}

// DeepCopy returns a deep copy of the SandboxAccess.
func (in *SandboxAccess) DeepCopy() *SandboxAccess {
	if in == nil {
		return nil
	}
	return &SandboxAccess{
		Read:  slices.Clone(in.Read),
		Write: slices.Clone(in.Write),
		Deny:  slices.Clone(in.Deny),
	}
}

// hasRules reports whether any access list is declared.
func (in *SandboxAccess) hasRules() bool {
	return in != nil && (in.Read != nil || in.Write != nil || len(in.Deny) > 0)
}

// SandboxExecMode controls command execution inside a sandbox.
type SandboxExecMode string

const (
	// SandboxExecNone forbids command execution.
	SandboxExecNone SandboxExecMode = "none"
	// SandboxExecAllowlist permits only the commands listed in allow.
	SandboxExecAllowlist SandboxExecMode = "allowlist"
	// SandboxExecAny permits any command (the default).
	SandboxExecAny SandboxExecMode = "any"
)

// SandboxExec declares command execution rules.
type SandboxExec struct {
	Mode  SandboxExecMode `yaml:"mode,omitempty" json:"mode,omitempty"`   // none, allowlist, or any (default)
	Allow []string        `yaml:"allow,omitempty" json:"allow,omitempty"` // Command base names or absolute paths permitted when mode is allowlist
}

// DeepCopy returns a deep copy of the SandboxExec.
func (in *SandboxExec) DeepCopy() *SandboxExec {
	if in == nil {
		return nil
	}
	return &SandboxExec{Mode: in.Mode, Allow: slices.Clone(in.Allow)}
}

// SandboxNetworkMode controls network egress from a sandbox.
type SandboxNetworkMode string

const (
	// SandboxNetworkNone blocks all egress (the default).
	SandboxNetworkNone SandboxNetworkMode = "none"
	// SandboxNetworkAllowlist permits only hosts listed in allow.
	SandboxNetworkAllowlist SandboxNetworkMode = "allowlist"
	// SandboxNetworkOpen permits any host not listed in deny.
	SandboxNetworkOpen SandboxNetworkMode = "open"
)

// SandboxNetwork declares network egress rules.
type SandboxNetwork struct {
	Mode  SandboxNetworkMode `yaml:"mode,omitempty" json:"mode,omitempty"`   // none (default), allowlist, or open
	Allow []string           `yaml:"allow,omitempty" json:"allow,omitempty"` // Host patterns allowed when mode is allowlist (e.g. *.pypi.org, registry.npmjs.org:443)
	Deny  []string           `yaml:"deny,omitempty" json:"deny,omitempty"`   // Host patterns that are always blocked
}

// DeepCopy returns a deep copy of the SandboxNetwork.
func (in *SandboxNetwork) DeepCopy() *SandboxNetwork {
	if in == nil {
		return nil
	}
	return &SandboxNetwork{Mode: in.Mode, Allow: slices.Clone(in.Allow), Deny: slices.Clone(in.Deny)}
}

// SandboxEnv declares environment exposure.
type SandboxEnv struct {
	Passthrough []string          `yaml:"passthrough,omitempty" json:"passthrough,omitempty"` // Globs of host environment variable names exposed to the sandbox
	Set         map[string]string `yaml:"set,omitempty" json:"set,omitempty"`                 // Variables injected into the sandbox (literals or ${<scheme>:<ref>} expressions)
}

// DeepCopy returns a deep copy of the SandboxEnv.
func (in *SandboxEnv) DeepCopy() *SandboxEnv {
	if in == nil {
		return nil
	}
	out := &SandboxEnv{Passthrough: slices.Clone(in.Passthrough)}
	if in.Set != nil {
		out.Set = make(map[string]string, len(in.Set))
		maps.Copy(out.Set, in.Set)
	}
	return out
}

// SandboxLimits declares resource limits.
type SandboxLimits struct {
	CPU       string `yaml:"cpu,omitempty" json:"cpu,omitempty"`             // CPU quota (e.g. "2", "500m")
	Memory    string `yaml:"memory,omitempty" json:"memory,omitempty"`       // Memory cap (e.g. "2Gi")
	Processes int    `yaml:"processes,omitempty" json:"processes,omitempty"` // Maximum number of concurrent processes
	Timeout   string `yaml:"timeout,omitempty" json:"timeout,omitempty"`     // Maximum lifetime of the sandbox (e.g. "30m")
}

// DeepCopy returns a deep copy of the SandboxLimits.
func (in *SandboxLimits) DeepCopy() *SandboxLimits {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// IsZero reports whether no limit is declared.
func (in *SandboxLimits) IsZero() bool {
	return in == nil || (in.CPU == "" && in.Memory == "" && in.Processes == 0 && in.Timeout == "")
}

// ─── Workspace sandbox ceiling ──────────────────────────────────────────────

// SandboxHostWrite controls whether host filesystems may be mounted rw.
type SandboxHostWrite string

const (
	// SandboxHostWriteAllow permits rw mounts of host filesystems (the default).
	SandboxHostWriteAllow SandboxHostWrite = "allow"
	// SandboxHostWriteDeny restricts host filesystems to ro mounts.
	SandboxHostWriteDeny SandboxHostWrite = "deny"
)

// DefaultSandboxHostPaths is the default value of SandboxPolicies.HostPaths.
var DefaultSandboxHostPaths = []string{"$WorkspaceDir/**"}

// SandboxPolicies is the workspace-wide, non-bypassable sandbox ceiling
// (workspace.spec.policies.sandbox). Agents, tools, and sandboxes can narrow
// it but never widen it.
type SandboxPolicies struct {
	Required  bool             `yaml:"required,omitempty" json:"required,omitempty"`   // Every command Tool and stdio MCP must resolve to a sandbox
	Drivers   *ResourceFilter  `yaml:"drivers,omitempty" json:"drivers,omitempty"`     // Glob filter over SandboxProvider and FileSystem driver names
	HostPaths []string         `yaml:"hostPaths,omitempty" json:"hostPaths,omitempty"` // Globs the rendered path of every host FileSystem must fall within (default ["$WorkspaceDir/**"])
	HostWrite SandboxHostWrite `yaml:"hostWrite,omitempty" json:"hostWrite,omitempty"` // allow (default) or deny rw mounts of host filesystems
	RootFS    *SandboxRootFS   `yaml:"rootfs,omitempty" json:"rootfs,omitempty"`       // Root filesystem ceiling
	Access    *SandboxAccess   `yaml:"access,omitempty" json:"access,omitempty"`       // Access rules ceiling
	Exec      *SandboxExec     `yaml:"exec,omitempty" json:"exec,omitempty"`           // Command execution ceiling
	Network   *SandboxNetwork  `yaml:"network,omitempty" json:"network,omitempty"`     // Network egress ceiling
	Env       *SandboxEnv      `yaml:"env,omitempty" json:"env,omitempty"`             // Environment ceiling (passthrough only)
	Limits    *SandboxLimits   `yaml:"limits,omitempty" json:"limits,omitempty"`       // Resource limits ceiling
}

// DeepCopy returns a deep copy of the SandboxPolicies.
func (in *SandboxPolicies) DeepCopy() *SandboxPolicies {
	if in == nil {
		return nil
	}
	out := new(SandboxPolicies)
	*out = *in
	out.Drivers = in.Drivers.DeepCopy()
	out.HostPaths = slices.Clone(in.HostPaths)
	out.RootFS = in.RootFS.DeepCopy()
	out.Access = in.Access.DeepCopy()
	out.Exec = in.Exec.DeepCopy()
	out.Network = in.Network.DeepCopy()
	out.Env = in.Env.DeepCopy()
	out.Limits = in.Limits.DeepCopy()
	return out
}

// EffectiveHostPaths returns HostPaths, or DefaultSandboxHostPaths when unset.
func (in *SandboxPolicies) EffectiveHostPaths() []string {
	if in == nil || len(in.HostPaths) == 0 {
		return slices.Clone(DefaultSandboxHostPaths)
	}
	return slices.Clone(in.HostPaths)
}

// AllowsDriver reports whether a SandboxProvider or FileSystem driver name is
// permitted by the drivers filter (include, then exclude, matched as globs).
func (in *SandboxPolicies) AllowsDriver(driver string) bool {
	if in == nil || in.Drivers == nil {
		return true
	}
	matchAny := func(patterns []string) bool {
		for _, pat := range patterns {
			if pat == "*" || pat == driver {
				return true
			}
			if matchSandboxGlob(pat, driver) {
				return true
			}
		}
		return false
	}
	if len(in.Drivers.Include) > 0 && !matchAny(in.Drivers.Include) {
		return false
	}
	if len(in.Drivers.Exclude) > 0 && matchAny(in.Drivers.Exclude) {
		return false
	}
	return true
}
