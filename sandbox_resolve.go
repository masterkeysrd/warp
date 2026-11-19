package warp

import (
	"errors"
	"fmt"
	"net"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SandboxAccessOp is a filesystem operation checked by ResolvedSandbox.AllowsPath.
type SandboxAccessOp string

const (
	// SandboxAccessRead is a read operation.
	SandboxAccessRead SandboxAccessOp = "read"
	// SandboxAccessWrite is a write operation. A permitted write implies read.
	SandboxAccessWrite SandboxAccessOp = "write"
)

// SandboxSource identifies which selection step chose a ResolvedSandbox.
type SandboxSource string

const (
	// SandboxSourceTool means the sandbox was selected by tool.spec.sandbox.
	SandboxSourceTool SandboxSource = "tool"
	// SandboxSourceAgent means the sandbox was selected by agent.spec.sandbox.
	SandboxSourceAgent SandboxSource = "agent"
	// SandboxSourceMCP means the sandbox was selected by mcp.spec.sandbox.
	SandboxSourceMCP SandboxSource = "mcp"
	// SandboxSourceWorkspace means the sandbox was selected by workspace.spec.defaultSandbox.
	SandboxSourceWorkspace SandboxSource = "workspace"
	// SandboxSourceSystem means the runtime-provided system/Sandbox/default was selected.
	SandboxSourceSystem SandboxSource = "system"
	// SandboxSourceDelegator means the agent selected no sandbox and the
	// delegating (parent) agent's effective sandbox was applied instead.
	SandboxSourceDelegator SandboxSource = "delegator"
	// SandboxSourceReference means the sandbox was resolved directly by
	// reference (ResolveSandbox).
	SandboxSourceReference SandboxSource = "reference"
)

// ErrNoDefaultSandbox is returned when no sandbox is selected explicitly and
// the runtime-provided system/Sandbox/default is not registered. Runtimes
// without sandbox support may execute unsandboxed in this case but must
// surface a clear warning; when workspace.spec.policies.sandbox.required is
// true the returned error is fatal.
var ErrNoDefaultSandbox = errors.New("no sandbox selected and the runtime-provided " + SandboxDefault + " is not registered")

// ResolvedSandbox is the effective sandbox for an agent, a tool invocation,
// or a stdio MCP server: the selected sandbox (provider, config, root
// filesystem, mounts, and workdir) combined with the rule sections intersected
// across the workspace ceiling, the agent's sandbox, the tool's sandbox, and
// (for subagents) the delegating agent's effective sandbox.
type ResolvedSandbox struct {
	// Sandbox is the selected sandbox with its extends chain merged.
	Sandbox *Sandbox
	// Provider is the resolved SandboxProvider, or nil when the runtime's
	// default provider applies.
	Provider *SandboxProvider
	// Driver is the provider's driver name, or "" when the runtime's default
	// provider applies and is unknown.
	Driver string
	// Config is the provider config shallow-merged with the sandbox config.
	Config map[string]any
	// RootFS is the effective (strictest) root filesystem mode. An empty value
	// means the driver default (rw when the driver supports rootfs.private,
	// otherwise ro); see ApplyDriverDefaults.
	RootFS SandboxRootFSMode
	// Mounts are the selected sandbox's mounts with resolved filesystems,
	// absolute paths, and effective modes.
	Mounts []ResolvedSandboxMount
	// Workdir is the absolute working directory.
	Workdir string
	// Access holds one access rule layer per contributing sandbox/ceiling.
	// Relative globs have been resolved against the workdir of the layer they
	// came from. An operation is allowed only if every layer allows it.
	Access []SandboxAccess
	// Exec holds the effective command execution rules.
	Exec ResolvedSandboxExec
	// Network holds the effective network egress rules.
	Network ResolvedSandboxNetwork
	// Env holds the effective environment exposure.
	Env ResolvedSandboxEnv
	// Limits holds the minimum of every declared limit across layers.
	Limits SandboxLimits
	// Source identifies the selection step that chose the sandbox.
	Source SandboxSource
	// Explicit is true when the sandbox is a guarantee (fail-closed): it was
	// selected explicitly, or policies.sandbox.required is true.
	Explicit bool
}

// ResolvedSandboxMount is a mount of a ResolvedSandbox.
type ResolvedSandboxMount struct {
	// FileSystem is the mounted filesystem.
	FileSystem *FileSystem
	// Path is the absolute mount target inside the sandbox.
	Path string
	// Mode is the effective mount mode (forced to ro by readOnly filesystems
	// and, for host filesystems, by policies.sandbox.hostWrite: deny).
	Mode SandboxMountMode
	// HostPath is the rendered host source path for host-backed filesystems
	// (host, or overlay over a host source); empty otherwise.
	HostPath string
}

// ResolvedSandboxExec is the effective command execution rule set.
type ResolvedSandboxExec struct {
	// Mode is the strictest mode across layers (none < allowlist < any).
	Mode SandboxExecMode
	// Allowlists holds one list per allowlist layer; a command must be allowed
	// by every list.
	Allowlists [][]string
}

// ResolvedSandboxNetwork is the effective network egress rule set.
type ResolvedSandboxNetwork struct {
	// Mode is the strictest mode across layers (none < allowlist < open).
	Mode SandboxNetworkMode
	// Allowlists holds one list per allowlist layer; a host must be allowed
	// by every list.
	Allowlists [][]string
	// Deny is the union of every layer's deny patterns.
	Deny []string
}

// ResolvedSandboxEnv is the effective environment exposure.
type ResolvedSandboxEnv struct {
	// Passthrough holds one glob list per layer; a host variable passes only
	// if it matches every list.
	Passthrough [][]string
	// Set holds injected variables merged across layers, the more specific
	// layer winning.
	Set map[string]string
	// Declared is true when any layer declares env.
	Declared bool
}

// ─── Lookup ─────────────────────────────────────────────────────────────────

// sandboxLookup resolves sandbox-related references against a Registry whose
// read lock is held by the caller.
type sandboxLookup struct {
	r *Registry
	// scope is the active project slug ("" for the workspace-root scope).
	scope string
	// all makes every namespace visible and prefers the referencing
	// resource's namespace (used by validation).
	all bool
}

func (l sandboxLookup) visible(ns string) bool {
	if l.all || (l.scope != "" && ns == l.scope) {
		return true
	}
	return l.r.isAllowedInBaseScopeLocked(ns)
}

func (l sandboxLookup) priority(ns, fromNS string) int {
	if l.scope != "" && ns == l.scope {
		return 200
	}
	if l.all && fromNS != "" && ns == fromNS && !isStandardNamespace(ns) {
		return 200
	}
	return NamespacePriority(ns)
}

func (l sandboxLookup) direct(kind Kind, ref string) Resource {
	parts := strings.Split(ref, "/")
	var qn string
	switch len(parts) {
	case 3:
		if Kind(parts[1]) != kind {
			return nil
		}
		qn = ref
	case 2:
		qn = MakeQualifiedName(parts[0], kind, parts[1])
	default:
		return nil
	}
	if res, ok := l.r.resources[qn]; ok && res.GetKind() == kind {
		return res
	}
	return nil
}

func (l sandboxLookup) best(kind Kind, fromNS string, names ...string) Resource {
	var best Resource
	bestPrio := -1
	for _, res := range l.r.resources {
		if res.GetKind() != kind || !slices.Contains(names, res.GetName()) {
			continue
		}
		ns := res.GetNamespace()
		if !l.visible(ns) {
			continue
		}
		p := l.priority(ns, fromNS)
		if p > bestPrio || (p == bestPrio && best != nil && res.QualifiedName() < best.QualifiedName()) {
			best = res
			bestPrio = p
		}
	}
	return best
}

// find resolves ref to a resource of kind following the resolution hierarchy:
// "local/..." aliases, qualified names ("namespace/Kind/name"),
// namespace-prefixed names ("namespace/name"), relative file paths, and short
// names (highest-priority namespace wins).
func (l sandboxLookup) find(kind Kind, ref, fromNS string) Resource {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if strings.HasPrefix(ref, NamespaceLocal+"/") {
		suffix := ref[len(NamespaceLocal):]
		var candidates []string
		if l.scope != "" {
			candidates = append(candidates, l.scope)
		}
		if fromNS != "" && fromNS != l.scope && !isStandardNamespace(fromNS) && fromNS != NamespaceLocal {
			candidates = append(candidates, fromNS)
		}
		candidates = append(candidates, NamespaceWorkspace)
		for _, ns := range candidates {
			if res := l.direct(kind, ns+suffix); res != nil {
				return res
			}
		}
		return nil
	}
	if strings.Contains(ref, "/") {
		if res := l.direct(kind, ref); res != nil {
			return res
		}
		// Relative file path references (e.g. "sandboxes/dev.yaml"). Unlike
		// generic resolution, namespace-prefixed names never fall back to a
		// same-named resource in another namespace.
		if isResourceFilePath(ref) {
			return l.best(kind, fromNS, slugifyPath(ref), slugifyPath(path.Base(ref)))
		}
		return nil
	}
	return l.best(kind, fromNS, ref, slugifyPath(ref))
}

func isResourceFilePath(ref string) bool {
	switch strings.ToLower(path.Ext(ref)) {
	case MarkdownExt, ".yaml", ".yml":
		return true
	}
	return false
}

func (l sandboxLookup) workspaceDef() *WorkspaceDef {
	if l.r.workspace == nil {
		return nil
	}
	return l.r.workspace.Def
}

func (l sandboxLookup) workspaceRoot() string {
	if l.r.workspace == nil {
		return ""
	}
	return l.r.workspace.RootPath
}

func (l sandboxLookup) policies() *SandboxPolicies {
	if def := l.workspaceDef(); def != nil && def.Spec.Policies != nil {
		return def.Spec.Policies.Sandbox
	}
	return nil
}

func (l sandboxLookup) required() bool {
	p := l.policies()
	return p != nil && p.Required
}

func (l sandboxLookup) defaultSandboxRef() string {
	if def := l.workspaceDef(); def != nil {
		return strings.TrimSpace(def.Spec.DefaultSandbox)
	}
	return ""
}

// projectDirs returns the candidate values of $ProjectDir for templated
// FileSystem configuration declared in namespace ns.
func (l sandboxLookup) projectDirs(ns string) []string {
	if l.scope != "" {
		if p, ok := l.r.projects[l.scope]; ok && p != nil {
			return []string{p.AbsPath()}
		}
	}
	if p, ok := l.r.projects[ns]; ok && p != nil {
		return []string{p.AbsPath()}
	}
	if len(l.r.projects) == 1 || (len(l.r.projects) > 1 && l.all) {
		dirs := make([]string, 0, len(l.r.projects))
		for _, p := range l.r.projects {
			dirs = append(dirs, p.AbsPath())
		}
		sort.Strings(dirs)
		return dirs
	}
	if root := l.workspaceRoot(); root != "" {
		return []string{root}
	}
	return []string{""}
}

// renderSandboxTemplate renders WARP templating ($WorkspaceDir, $ProjectDir)
// in a sandbox or filesystem configuration value. ok is false when the value
// references variables that are unknown or empty, or contains unresolved
// ${<scheme>:<ref>} expressions.
func renderSandboxTemplate(s, workspaceDir, projectDir string) (string, bool) {
	if !strings.Contains(s, "$") && !strings.Contains(s, "{{") {
		return s, true
	}
	data := map[string]any{}
	if workspaceDir != "" {
		data["WorkspaceDir"] = workspaceDir
		data["WorkspacePath"] = filepath.Join(workspaceDir, WorkspaceFileName)
		data["Workspace"] = TemplateWorkspace{Dir: workspaceDir, Path: filepath.Join(workspaceDir, WorkspaceFileName)}
	}
	if projectDir != "" {
		data["ProjectDir"] = projectDir
		data["Project"] = TemplateProject{Dir: projectDir}
	}
	out, err := renderTemplate("sandbox-config", s, data)
	if err != nil || strings.Contains(out, "<no value>") || strings.Contains(out, "${") {
		return "", false
	}
	return out, true
}

// hostSource returns whether fsr is host-backed (a host filesystem or an
// overlay whose source chain ends in a host filesystem) and, when it is, the
// rendered absolute host path for the given $ProjectDir. ok is false when the
// path cannot be rendered.
func (l sandboxLookup) hostSource(fsr *FileSystem, projectDir string) (hostPath string, hostBacked, ok bool) {
	visited := map[string]bool{}
	curr := fsr
	for curr != nil {
		qn := curr.QualifiedName()
		if visited[qn] {
			return "", false, false
		}
		visited[qn] = true
		switch curr.Spec.Driver {
		case FileSystemDriverHost:
			raw := curr.Spec.ConfigString("path")
			if raw == "" {
				return "", true, false
			}
			rendered, rok := renderSandboxTemplate(raw, l.workspaceRoot(), projectDir)
			if !rok || rendered == "" {
				return "", true, false
			}
			if !filepath.IsAbs(rendered) {
				base := projectDir
				if base == "" {
					base = l.workspaceRoot()
				}
				if base == "" {
					return "", true, false
				}
				rendered = filepath.Join(base, rendered)
			}
			return filepath.ToSlash(filepath.Clean(rendered)), true, true
		case FileSystemDriverOverlay:
			src := curr.Spec.ConfigString("source")
			next, _ := l.find(KindFileSystem, src, curr.GetNamespace()).(*FileSystem)
			curr = next
		default:
			return "", false, true
		}
	}
	return "", false, true
}

// hostPathsApply reports whether the hostPaths ceiling can be evaluated: the
// workspace root is known (for the default "$WorkspaceDir/**"), or hostPaths
// is declared explicitly.
func (l sandboxLookup) hostPathsApply() bool {
	if l.workspaceRoot() != "" {
		return true
	}
	p := l.policies()
	return p != nil && len(p.HostPaths) > 0
}

// withinHostPaths reports whether hostPath matches one of the rendered
// hostPaths globs (relative globs resolve against the workspace root). ok is
// false when a glob cannot be rendered.
func (l sandboxLookup) withinHostPaths(hostPath, projectDir string) (within, ok bool) {
	ok = true
	for _, pat := range l.policies().EffectiveHostPaths() {
		rendered, rok := renderSandboxTemplate(pat, l.workspaceRoot(), projectDir)
		if !rok {
			ok = false
			continue
		}
		if !filepath.IsAbs(rendered) && l.workspaceRoot() != "" {
			rendered = filepath.Join(l.workspaceRoot(), rendered)
		}
		rendered = filepath.ToSlash(filepath.Clean(rendered))
		if rendered == hostPath || matchSandboxGlob(rendered, hostPath) {
			return true, true
		}
	}
	return false, ok
}

// ─── Sandbox inheritance ────────────────────────────────────────────────────

// mergedSandbox is a Sandbox with its extends chain merged, remembering the
// namespace each inherited reference must be resolved from.
type mergedSandbox struct {
	sb         *Sandbox
	providerNS string
	mountNS    []string
}

// chain resolves ref and merges its extends chain (parent first). Mounts are
// merged by path, config is shallow-merged, and provider, rootfs, workdir,
// access, exec, network, env, and limits are replaced as a whole when
// specified on the child. Circular chains return an error.
func (l sandboxLookup) chain(ref, fromNS string, visited map[string]bool) (*mergedSandbox, error) {
	res := l.find(KindSandbox, ref, fromNS)
	if res == nil {
		return nil, fmt.Errorf("sandbox %q not found", ref)
	}
	sb, ok := res.(*Sandbox)
	if !ok {
		return nil, fmt.Errorf("%q is not a Sandbox resource", ref)
	}
	qn := sb.QualifiedName()
	if visited[qn] {
		return nil, fmt.Errorf("circular sandbox inheritance detected at %q", qn)
	}
	ns := sb.GetNamespace()
	if sb.Spec.Extends == "" {
		out := &mergedSandbox{sb: sb.DeepCopy(), providerNS: ns}
		for range sb.Spec.Mounts {
			out.mountNS = append(out.mountNS, ns)
		}
		return out, nil
	}
	visited[qn] = true
	parent, err := l.chain(sb.Spec.Extends, ns, visited)
	if err != nil {
		return nil, fmt.Errorf("sandbox %q: %w", qn, err)
	}

	merged := parent.sb
	merged.BaseResource = *sb.BaseResource.DeepCopy()
	merged.Spec.Enabled = copyBoolPtr(sb.Spec.Enabled)
	merged.Spec.Extends = sb.Spec.Extends

	// 1. Mounts merged by path.
	for _, m := range sb.Spec.Mounts {
		key := l.mountKey(m, ns)
		replaced := false
		for i, pm := range merged.Spec.Mounts {
			if l.mountKey(pm, parent.mountNS[i]) == key {
				merged.Spec.Mounts[i] = m
				parent.mountNS[i] = ns
				replaced = true
				break
			}
		}
		if !replaced {
			merged.Spec.Mounts = append(merged.Spec.Mounts, m)
			parent.mountNS = append(parent.mountNS, ns)
		}
	}
	// 2. Config shallow-merged.
	if len(sb.Spec.Config) > 0 {
		if merged.Spec.Config == nil {
			merged.Spec.Config = make(map[string]any, len(sb.Spec.Config))
		}
		for k, v := range sb.Spec.Config {
			merged.Spec.Config[k] = deepCopyConfigValue(v)
		}
	}
	// 3. Whole-section replacement.
	if sb.Spec.Provider != "" {
		merged.Spec.Provider = sb.Spec.Provider
		parent.providerNS = ns
	}
	if sb.Spec.RootFS != nil {
		merged.Spec.RootFS = sb.Spec.RootFS.DeepCopy()
	}
	if sb.Spec.Workdir != "" {
		merged.Spec.Workdir = sb.Spec.Workdir
	}
	if sb.Spec.Access != nil {
		merged.Spec.Access = sb.Spec.Access.DeepCopy()
	}
	if sb.Spec.Exec != nil {
		merged.Spec.Exec = sb.Spec.Exec.DeepCopy()
	}
	if sb.Spec.Network != nil {
		merged.Spec.Network = sb.Spec.Network.DeepCopy()
	}
	if sb.Spec.Env != nil {
		merged.Spec.Env = sb.Spec.Env.DeepCopy()
	}
	if sb.Spec.Limits != nil {
		merged.Spec.Limits = sb.Spec.Limits.DeepCopy()
	}
	return parent, nil
}

// mountKey returns the key used to merge mounts by path: the declared path,
// or the default (rendered host source) path for host-backed filesystems.
func (l sandboxLookup) mountKey(m SandboxMount, ns string) string {
	if m.Path != "" {
		return path.Clean(m.Path)
	}
	if fsr, ok := l.find(KindFileSystem, m.FileSystem, ns).(*FileSystem); ok {
		if hp, hostBacked, hok := l.hostSource(fsr, firstOr(l.projectDirs(fsr.GetNamespace()), "")); hostBacked && hok {
			return hp
		}
	}
	return "filesystem:" + m.FileSystem
}

func firstOr(s []string, def string) string {
	if len(s) > 0 {
		return s[0]
	}
	return def
}

// ─── Building a ResolvedSandbox ─────────────────────────────────────────────

// sandboxBuild describes the inputs of a ResolvedSandbox: the selected sandbox
// and the ordered rule layers (least specific first).
type sandboxBuild struct {
	selected *mergedSandbox
	layers   []*mergedSandbox
	source   SandboxSource
	explicit bool
	// strict makes unresolvable references and policy violations errors.
	// Validation builds non-strict views and reports those issues separately.
	strict bool
}

func (l sandboxLookup) build(b sandboxBuild) (*ResolvedSandbox, error) {
	sel := b.selected
	sb := sel.sb
	ceiling := l.policies()
	rs := &ResolvedSandbox{
		Sandbox:  sb,
		Source:   b.source,
		Explicit: b.explicit,
	}

	// Provider and driver.
	if err := l.resolveProvider(rs, sel, ceiling, b.strict); err != nil {
		return nil, err
	}

	// Mounts.
	for i, m := range sb.Spec.Mounts {
		ns := sb.GetNamespace()
		if i < len(sel.mountNS) {
			ns = sel.mountNS[i]
		}
		fsr, _ := l.find(KindFileSystem, m.FileSystem, ns).(*FileSystem)
		if fsr == nil {
			if b.strict {
				return nil, fmt.Errorf("sandbox %q: mount filesystem %q not found", sb.QualifiedName(), m.FileSystem)
			}
			continue
		}
		if !fsr.Spec.IsEnabled() && b.strict {
			return nil, fmt.Errorf("sandbox %q: mount filesystem %q is disabled", sb.QualifiedName(), fsr.QualifiedName())
		}
		if b.strict && !ceiling.AllowsDriver(fsr.Spec.Driver) {
			return nil, fmt.Errorf("sandbox %q: filesystem %q driver %q is not allowed by workspace policies.sandbox.drivers", sb.QualifiedName(), fsr.QualifiedName(), fsr.Spec.Driver)
		}
		projectDir := firstOr(l.projectDirs(fsr.GetNamespace()), "")
		hostPath, hostBacked, hok := l.hostSource(fsr, projectDir)
		if hostBacked && b.strict {
			if !hok {
				return nil, fmt.Errorf("sandbox %q: host path of filesystem %q cannot be rendered", sb.QualifiedName(), fsr.QualifiedName())
			}
			if l.hostPathsApply() {
				if within, _ := l.withinHostPaths(hostPath, projectDir); !within {
					return nil, fmt.Errorf("sandbox %q: host path %q of filesystem %q is outside workspace policies.sandbox.hostPaths", sb.QualifiedName(), hostPath, fsr.QualifiedName())
				}
			}
		}
		mount := ResolvedSandboxMount{FileSystem: fsr, Mode: m.Mode}
		if hostBacked && hok {
			mount.HostPath = hostPath
		}
		if mount.Mode != SandboxMountReadWrite {
			mount.Mode = SandboxMountReadOnly
		}
		if fsr.Spec.ReadOnly || (fsr.Spec.Driver == FileSystemDriverHost && ceiling != nil && ceiling.HostWrite == SandboxHostWriteDeny) {
			mount.Mode = SandboxMountReadOnly
		}
		switch {
		case m.Path != "":
			mount.Path = path.Clean(m.Path)
		case mount.HostPath != "":
			mount.Path = mount.HostPath
		default:
			if b.strict {
				return nil, fmt.Errorf("sandbox %q: mount of filesystem %q requires a path", sb.QualifiedName(), fsr.QualifiedName())
			}
			continue
		}
		rs.Mounts = append(rs.Mounts, mount)
	}

	// Workdir (4-step default).
	rs.Workdir = defaultSandboxWorkdir(sb.Spec.Workdir, rs.Mounts)

	// Root filesystem: strictest across layers, unset sandbox layers count as
	// the driver default (between ro and rw).
	rootRank := rootFSRank(ceilingRootFS(ceiling), true)
	for _, layer := range b.layers {
		mode := SandboxRootFSMode("")
		if layer.sb.Spec.RootFS != nil {
			mode = layer.sb.Spec.RootFS.Mode
		}
		rootRank = min(rootRank, rootFSRank(mode, false))
	}
	rs.RootFS = rootFSFromRank(rootRank)

	// Access, exec, network, env, limits.
	if ceiling != nil && ceiling.Access.hasRules() {
		rs.Access = append(rs.Access, absolutizeAccess(ceiling.Access, rs.Workdir))
	}
	rs.Exec.Mode = SandboxExecAny
	rs.Network.Mode = SandboxNetworkOpen
	if ceiling != nil {
		rs.applyExec(ceiling.Exec, true)
		rs.applyNetwork(ceiling.Network, true)
		if ceiling.Env != nil {
			rs.Env.Declared = true
			rs.Env.Passthrough = append(rs.Env.Passthrough, slices.Clone(ceiling.Env.Passthrough))
		}
		rs.Limits = minSandboxLimits(rs.Limits, ceiling.Limits)
	}
	for _, layer := range b.layers {
		spec := &layer.sb.Spec
		if spec.Access.hasRules() {
			rs.Access = append(rs.Access, absolutizeAccess(spec.Access, layerWorkdir(l, layer, rs, sel)))
		}
		rs.applyExec(spec.Exec, false)
		rs.applyNetwork(spec.Network, false)
		if spec.Env != nil {
			rs.Env.Declared = true
			rs.Env.Passthrough = append(rs.Env.Passthrough, slices.Clone(spec.Env.Passthrough))
			for k, v := range spec.Env.Set {
				if rs.Env.Set == nil {
					rs.Env.Set = make(map[string]string)
				}
				rs.Env.Set[k] = v
			}
		} else {
			// When env is omitted no host variables pass through this layer.
			rs.Env.Passthrough = append(rs.Env.Passthrough, []string{})
		}
		rs.Limits = minSandboxLimits(rs.Limits, spec.Limits)
	}
	return rs, nil
}

// layerWorkdir returns the workdir used to resolve a layer's relative access
// globs: the effective workdir for the selected sandbox, or the layer's own
// workdir (with the same default rule) for other layers.
func layerWorkdir(l sandboxLookup, layer *mergedSandbox, rs *ResolvedSandbox, sel *mergedSandbox) string {
	if layer == sel || layer.sb.QualifiedName() == sel.sb.QualifiedName() {
		return rs.Workdir
	}
	var mounts []ResolvedSandboxMount
	for i, m := range layer.sb.Spec.Mounts {
		ns := layer.sb.GetNamespace()
		if i < len(layer.mountNS) {
			ns = layer.mountNS[i]
		}
		fsr, _ := l.find(KindFileSystem, m.FileSystem, ns).(*FileSystem)
		if fsr == nil {
			continue
		}
		p := m.Path
		if p == "" {
			hp, hostBacked, ok := l.hostSource(fsr, firstOr(l.projectDirs(fsr.GetNamespace()), ""))
			if !hostBacked || !ok {
				continue
			}
			p = hp
		}
		mounts = append(mounts, ResolvedSandboxMount{FileSystem: fsr, Path: path.Clean(p)})
	}
	return defaultSandboxWorkdir(layer.sb.Spec.Workdir, mounts)
}

// defaultSandboxWorkdir applies the workdir default rule: the declared (or
// inherited) workdir, else the mount path of system/FileSystem/cwd, else the
// path of the only mount, else "/".
func defaultSandboxWorkdir(declared string, mounts []ResolvedSandboxMount) string {
	if declared != "" {
		return path.Clean(declared)
	}
	for _, m := range mounts {
		if m.FileSystem != nil && m.FileSystem.QualifiedName() == FileSystemCwd {
			return m.Path
		}
	}
	if len(mounts) == 1 {
		return mounts[0].Path
	}
	return "/"
}

func (l sandboxLookup) resolveProvider(rs *ResolvedSandbox, sel *mergedSandbox, ceiling *SandboxPolicies, strict bool) error {
	sb := sel.sb
	var provider *SandboxProvider
	if ref := sb.Spec.Provider; ref != "" {
		provider, _ = l.find(KindSandboxProvider, ref, sel.providerNS).(*SandboxProvider)
		if provider == nil && strict {
			return fmt.Errorf("sandbox %q: provider %q not found", sb.QualifiedName(), ref)
		}
	} else if sb.QualifiedName() != SandboxDefault {
		// No provider anywhere in the chain: the runtime's default provider
		// (the one used by system/Sandbox/default) applies.
		if def, err := l.chain(SandboxDefault, NamespaceSystem, map[string]bool{}); err == nil && def.sb.Spec.Provider != "" {
			provider, _ = l.find(KindSandboxProvider, def.sb.Spec.Provider, def.providerNS).(*SandboxProvider)
		}
	}
	if provider != nil {
		if strict && !provider.Spec.IsEnabled() {
			return fmt.Errorf("sandbox %q: provider %q is disabled", sb.QualifiedName(), provider.QualifiedName())
		}
		if strict && !ceiling.AllowsDriver(provider.Spec.Driver) {
			return fmt.Errorf("sandbox %q: provider %q driver %q is not allowed by workspace policies.sandbox.drivers", sb.QualifiedName(), provider.QualifiedName(), provider.Spec.Driver)
		}
		rs.Provider = provider
		rs.Driver = provider.Spec.Driver
		rs.Config = deepCopyConfig(provider.Spec.Config)
	}
	if len(sb.Spec.Config) > 0 {
		if rs.Config == nil {
			rs.Config = make(map[string]any, len(sb.Spec.Config))
		}
		for k, v := range sb.Spec.Config {
			rs.Config[k] = deepCopyConfigValue(v)
		}
	}
	return nil
}

func ceilingRootFS(c *SandboxPolicies) SandboxRootFSMode {
	if c == nil || c.RootFS == nil {
		return ""
	}
	return c.RootFS.Mode
}

// rootFSRank orders root filesystem modes: none < ro < driver default < rw.
// For ceilings an unset mode imposes no constraint.
func rootFSRank(mode SandboxRootFSMode, ceiling bool) int {
	switch mode {
	case SandboxRootFSNone:
		return 0
	case SandboxRootFSReadOnly:
		return 1
	case SandboxRootFSReadWrite:
		return 3
	}
	if ceiling {
		return 4
	}
	return 2
}

func rootFSFromRank(rank int) SandboxRootFSMode {
	switch rank {
	case 0:
		return SandboxRootFSNone
	case 1:
		return SandboxRootFSReadOnly
	case 3:
		return SandboxRootFSReadWrite
	}
	return ""
}

func execRank(m SandboxExecMode) int {
	switch m {
	case SandboxExecNone:
		return 0
	case SandboxExecAllowlist:
		return 1
	}
	return 2
}

func networkRank(m SandboxNetworkMode) int {
	switch m {
	case SandboxNetworkNone:
		return 0
	case SandboxNetworkAllowlist:
		return 1
	}
	return 2
}

// applyExec intersects one exec layer. For sandbox layers an unset section
// or mode means "any"; for the ceiling an unset mode imposes no constraint
// unless allow is declared, in which case it acts as an allowlist.
func (rs *ResolvedSandbox) applyExec(e *SandboxExec, ceiling bool) {
	mode := SandboxExecAny
	if e != nil {
		mode = e.Mode
		if mode == "" {
			mode = SandboxExecAny
			if ceiling && len(e.Allow) > 0 {
				mode = SandboxExecAllowlist
			}
		}
	}
	if execRank(mode) < execRank(rs.Exec.Mode) {
		rs.Exec.Mode = mode
	}
	if mode == SandboxExecAllowlist {
		rs.Exec.Allowlists = append(rs.Exec.Allowlists, slices.Clone(e.Allow))
	}
}

// applyNetwork intersects one network layer. For sandbox layers an unset
// section or mode means "none"; for the ceiling an unset section imposes no
// constraint and an unset mode is "open" unless allow is declared, in which
// case it acts as an allowlist.
func (rs *ResolvedSandbox) applyNetwork(n *SandboxNetwork, ceiling bool) {
	var mode SandboxNetworkMode
	switch {
	case n == nil && ceiling:
		return
	case n == nil:
		mode = SandboxNetworkNone
	case n.Mode != "":
		mode = n.Mode
	case ceiling && len(n.Allow) > 0:
		mode = SandboxNetworkAllowlist
	case ceiling:
		mode = SandboxNetworkOpen
	default:
		mode = SandboxNetworkNone
	}
	if networkRank(mode) < networkRank(rs.Network.Mode) {
		rs.Network.Mode = mode
	}
	if n == nil {
		return
	}
	if mode == SandboxNetworkAllowlist {
		rs.Network.Allowlists = append(rs.Network.Allowlists, slices.Clone(n.Allow))
	}
	for _, d := range n.Deny {
		if !slices.Contains(rs.Network.Deny, d) {
			rs.Network.Deny = append(rs.Network.Deny, d)
		}
	}
}

// absolutizeAccess resolves relative access globs against workdir.
func absolutizeAccess(a *SandboxAccess, workdir string) SandboxAccess {
	abs := func(globs []string) []string {
		if globs == nil {
			return nil
		}
		out := make([]string, 0, len(globs))
		for _, g := range globs {
			out = append(out, absoluteSandboxPath(g, workdir))
		}
		return out
	}
	return SandboxAccess{Read: abs(a.Read), Write: abs(a.Write), Deny: abs(a.Deny)}
}

func absoluteSandboxPath(p, workdir string) string {
	if strings.HasPrefix(p, "/") {
		return path.Clean(p)
	}
	if workdir == "" {
		workdir = "/"
	}
	return path.Join(workdir, p)
}

// ─── Limits ─────────────────────────────────────────────────────────────────

// minSandboxLimits returns the per-field minimum of a and b. Values that
// cannot be parsed keep the earlier (less specific, e.g. ceiling) value.
func minSandboxLimits(a SandboxLimits, b *SandboxLimits) SandboxLimits {
	if b == nil {
		return a
	}
	a.CPU = minQuantity(a.CPU, b.CPU, parseCPUQuantity)
	a.Memory = minQuantity(a.Memory, b.Memory, parseMemoryQuantity)
	a.Timeout = minQuantity(a.Timeout, b.Timeout, parseTimeoutQuantity)
	switch {
	case a.Processes == 0:
		a.Processes = b.Processes
	case b.Processes > 0 && b.Processes < a.Processes:
		a.Processes = b.Processes
	}
	return a
}

func minQuantity(a, b string, parse func(string) (float64, bool)) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	av, aok := parse(a)
	bv, bok := parse(b)
	if aok && bok && bv < av {
		return b
	}
	return a
}

func parseCPUQuantity(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if m, ok := strings.CutSuffix(s, "m"); ok {
		v, err := strconv.ParseFloat(m, 64)
		return v / 1000, err == nil
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

func parseMemoryQuantity(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		mult   float64
	}{
		{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40}, {"Pi", 1 << 50},
		{"k", 1e3}, {"K", 1e3}, {"M", 1e6}, {"G", 1e9}, {"T", 1e12}, {"P", 1e15},
	}
	for _, u := range units {
		if num, ok := strings.CutSuffix(s, u.suffix); ok {
			v, err := strconv.ParseFloat(num, 64)
			return v * u.mult, err == nil
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

func parseTimeoutQuantity(s string) (float64, bool) {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	return float64(d), err == nil
}

// ─── Enforcement helpers ────────────────────────────────────────────────────

// AllowsPath reports whether op (read or write) on p is permitted. Relative
// paths resolve against Workdir. Evaluation, for every access layer: a deny
// match denies; then the layer's mode (the containing mount's mode, or the
// root filesystem mode) must permit the operation; then, when the list for
// the operation is set, the path must match it (write matches also grant
// read). A nil receiver (no sandbox) permits everything.
func (rs *ResolvedSandbox) AllowsPath(p string, op SandboxAccessOp) bool {
	if rs == nil {
		return true
	}
	if op != SandboxAccessRead && op != SandboxAccessWrite {
		return false
	}
	p = absoluteSandboxPath(p, rs.Workdir)

	// 1. Deny always wins.
	for _, layer := range rs.Access {
		if matchAnySandboxGlob(layer.Deny, p) {
			return false
		}
	}
	// 2. The filesystem layer's mode.
	if !rs.modePermits(p, op) {
		return false
	}
	// 3. Operation lists.
	for _, layer := range rs.Access {
		switch op {
		case SandboxAccessRead:
			if layer.Read != nil && !matchAnySandboxGlob(layer.Read, p) && !matchAnySandboxGlob(layer.Write, p) {
				return false
			}
		case SandboxAccessWrite:
			if layer.Write != nil && !matchAnySandboxGlob(layer.Write, p) {
				return false
			}
		}
	}
	// 4. Otherwise the layer's mode decides (checked above).
	return true
}

// modePermits checks the mode of the mount containing p (longest match), or
// of the root filesystem when no mount contains p. An unknown driver-default
// root filesystem mode is treated as read-only.
func (rs *ResolvedSandbox) modePermits(p string, op SandboxAccessOp) bool {
	var best *ResolvedSandboxMount
	for i := range rs.Mounts {
		m := &rs.Mounts[i]
		if pathWithin(p, m.Path) && (best == nil || len(m.Path) > len(best.Path)) {
			best = m
		}
	}
	if best != nil {
		return op == SandboxAccessRead || best.Mode == SandboxMountReadWrite
	}
	switch rs.RootFS {
	case SandboxRootFSReadWrite:
		return true
	case SandboxRootFSNone:
		if op != SandboxAccessRead {
			return false
		}
		// "/" is an empty directory containing only mount points.
		for _, m := range rs.Mounts {
			if pathWithin(m.Path, p) {
				return true
			}
		}
		return false
	default:
		return op == SandboxAccessRead
	}
}

// pathWithin reports whether p equals dir or is nested below it.
func pathWithin(p, dir string) bool {
	if dir == "/" {
		return strings.HasPrefix(p, "/")
	}
	return p == dir || strings.HasPrefix(p, dir+"/")
}

func matchAnySandboxGlob(globs []string, p string) bool {
	for _, g := range globs {
		if g == p || matchSandboxGlob(g, p) {
			return true
		}
	}
	return false
}

// AllowsCommand reports whether the command name (a base name or an absolute
// path) may be executed. With exec.mode none nothing may run; with allowlist
// the command must be allowed by every allowlist layer, where entries are
// matched against the executable's base name, or exactly when the entry is an
// absolute path. A nil receiver (no sandbox) permits everything.
func (rs *ResolvedSandbox) AllowsCommand(name string) bool {
	if rs == nil {
		return true
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	switch rs.Exec.Mode {
	case SandboxExecNone:
		return false
	case SandboxExecAllowlist:
		for _, list := range rs.Exec.Allowlists {
			if !commandInAllowlist(list, name) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

func commandInAllowlist(list []string, name string) bool {
	base := path.Base(filepath.ToSlash(name))
	for _, entry := range list {
		if strings.Contains(entry, "/") {
			if path.Clean(entry) == path.Clean(filepath.ToSlash(name)) {
				return true
			}
			continue
		}
		if entry == base {
			return true
		}
	}
	return false
}

// AllowsHost reports whether egress to host (optionally "host:port") is
// permitted. Deny patterns in any layer always block; with network.mode none
// nothing is reachable; with allowlist the host must match every allowlist
// layer. Patterns may use a leading "*." wildcard (matching one or more
// subdomain labels) and an optional ":port". A nil receiver (no sandbox)
// permits everything.
func (rs *ResolvedSandbox) AllowsHost(host string) bool {
	if rs == nil {
		return true
	}
	h, port := splitHostPort(host)
	if h == "" {
		return false
	}
	for _, pat := range rs.Network.Deny {
		if matchHostPattern(pat, h, port, true) {
			return false
		}
	}
	switch rs.Network.Mode {
	case SandboxNetworkNone:
		return false
	case SandboxNetworkAllowlist:
		for _, list := range rs.Network.Allowlists {
			allowed := false
			for _, pat := range list {
				if matchHostPattern(pat, h, port, false) {
					allowed = true
					break
				}
			}
			if !allowed {
				return false
			}
		}
		return true
	default:
		return true
	}
}

func splitHostPort(s string) (host, port string) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") {
		if h, p, err := net.SplitHostPort(s); err == nil {
			return strings.ToLower(h), p
		}
		return strings.ToLower(strings.Trim(s, "[]")), ""
	}
	if strings.Count(s, ":") == 1 {
		h, p, _ := strings.Cut(s, ":")
		return strings.ToLower(h), p
	}
	return strings.ToLower(s), ""
}

// matchHostPattern matches a host pattern against a host and optional port.
// A pattern port must equal the host port; for deny patterns a host without a
// port also matches (conservative), for allow patterns it does not.
func matchHostPattern(pattern, host, port string, deny bool) bool {
	ph, pp := splitHostPort(pattern)
	if ph == "" {
		return false
	}
	if pp != "" {
		if port == "" {
			if !deny {
				return false
			}
		} else if pp != port {
			return false
		}
	}
	if ph == "*" {
		return true
	}
	if suffix, ok := strings.CutPrefix(ph, "*."); ok {
		return strings.HasSuffix(host, "."+suffix) && len(host) > len(suffix)+1
	}
	return ph == host
}

// AllowsEnv reports whether the host environment variable name passes into
// the sandbox (it must match a passthrough glob in every layer). A nil
// receiver (no sandbox) permits everything.
func (rs *ResolvedSandbox) AllowsEnv(name string) bool {
	if rs == nil {
		return true
	}
	if len(rs.Env.Passthrough) == 0 {
		return false
	}
	for _, list := range rs.Env.Passthrough {
		matched := false
		for _, g := range list {
			if ok, _ := path.Match(g, name); ok || g == name {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// ─── Capabilities ───────────────────────────────────────────────────────────

// RequiredCapabilities returns the driver capabilities required by the
// features the effective sandbox uses (Section 12.6 capability table).
// needsExec must be true when the sandbox runs a Tool.spec.command or a stdio
// MCP server. An empty (driver-default) root filesystem mode requires no
// capability. A network deny list with mode open requires network.allowlist
// because host filtering must be enforced.
func (rs *ResolvedSandbox) RequiredCapabilities(needsExec bool) []string {
	if rs == nil {
		return nil
	}
	var caps []string
	add := func(c string) {
		if !slices.Contains(caps, c) {
			caps = append(caps, c)
		}
	}
	if needsExec {
		add(SandboxCapabilityExec)
	}
	if rs.Exec.Mode == SandboxExecAllowlist {
		add(SandboxCapabilityExecAllowlist)
	}
	switch rs.RootFS {
	case SandboxRootFSReadWrite:
		add(SandboxCapabilityRootFSPrivate)
	case SandboxRootFSReadOnly:
		add(SandboxCapabilityRootFSReadOnly)
	case SandboxRootFSNone:
		add(SandboxCapabilityRootFSNone)
	}
	if len(rs.Mounts) > 0 {
		add(SandboxCapabilityMounts)
	}
	for _, m := range rs.Mounts {
		if m.HostPath != "" && m.Path != m.HostPath {
			add(SandboxCapabilityMountsRemap)
			break
		}
	}
	if len(rs.Access) > 0 {
		add(SandboxCapabilityAccessRules)
	}
	switch rs.Network.Mode {
	case SandboxNetworkNone:
		add(SandboxCapabilityNetworkNone)
	case SandboxNetworkAllowlist:
		add(SandboxCapabilityNetworkAllowlist)
	case SandboxNetworkOpen:
		if len(rs.Network.Deny) > 0 {
			add(SandboxCapabilityNetworkAllowlist)
		}
	}
	if rs.Env.Declared {
		add(SandboxCapabilityEnvIsolation)
	}
	if rs.Limits.CPU != "" {
		add(SandboxCapabilityLimitsCPU)
	}
	if rs.Limits.Memory != "" {
		add(SandboxCapabilityLimitsMemory)
	}
	if rs.Limits.Processes != 0 {
		add(SandboxCapabilityLimitsProcesses)
	}
	if rs.Limits.Timeout != "" {
		add(SandboxCapabilityLimitsTimeout)
	}
	return caps
}

// CheckCapabilities verifies that info reports every capability required by
// the effective sandbox (see RequiredCapabilities). It returns an error
// listing the missing capabilities, in which case the runtime must not start
// the sandbox.
func (rs *ResolvedSandbox) CheckCapabilities(info DriverInfo, needsExec bool) error {
	missing := info.MissingCapabilities(rs.RequiredCapabilities(needsExec))
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("sandbox driver %q lacks required capabilities: %s", rs.Driver, strings.Join(missing, ", "))
}

// ApplyDriverDefaults returns a copy of rs with driver-dependent defaults
// resolved from info: an unset root filesystem mode becomes rw when the driver
// supports rootfs.private, otherwise ro.
func (rs *ResolvedSandbox) ApplyDriverDefaults(info DriverInfo) *ResolvedSandbox {
	if rs == nil {
		return nil
	}
	out := rs.DeepCopy()
	if out.RootFS == "" {
		if info.HasCapability(SandboxCapabilityRootFSPrivate) {
			out.RootFS = SandboxRootFSReadWrite
		} else {
			out.RootFS = SandboxRootFSReadOnly
		}
	}
	return out
}

// DeepCopy returns a deep copy of the ResolvedSandbox. Referenced resources
// (Sandbox, Provider, mount FileSystems) are deep-copied as well.
func (rs *ResolvedSandbox) DeepCopy() *ResolvedSandbox {
	if rs == nil {
		return nil
	}
	out := *rs
	out.Sandbox = rs.Sandbox.DeepCopy()
	out.Provider = rs.Provider.DeepCopy()
	out.Config = deepCopyConfig(rs.Config)
	if rs.Mounts != nil {
		out.Mounts = make([]ResolvedSandboxMount, len(rs.Mounts))
		for i, m := range rs.Mounts {
			m.FileSystem = m.FileSystem.DeepCopy()
			out.Mounts[i] = m
		}
	}
	if rs.Access != nil {
		out.Access = make([]SandboxAccess, len(rs.Access))
		for i, a := range rs.Access {
			out.Access[i] = *a.DeepCopy()
		}
	}
	out.Exec.Allowlists = cloneStringLists(rs.Exec.Allowlists)
	out.Network.Allowlists = cloneStringLists(rs.Network.Allowlists)
	out.Network.Deny = slices.Clone(rs.Network.Deny)
	out.Env.Passthrough = cloneStringLists(rs.Env.Passthrough)
	if rs.Env.Set != nil {
		out.Env.Set = make(map[string]string, len(rs.Env.Set))
		for k, v := range rs.Env.Set {
			out.Env.Set[k] = v
		}
	}
	return &out
}

func cloneStringLists(in [][]string) [][]string {
	if in == nil {
		return nil
	}
	out := make([][]string, len(in))
	for i, l := range in {
		out[i] = slices.Clone(l)
	}
	return out
}

// Intersect returns a copy of rs whose rule sections (rootfs, access, exec,
// network, env passthrough, and limits) are additionally intersected with
// other's effective rules, so that the result never grants more than either.
// The selection (provider, config, mounts, workdir) and env.set of rs are
// kept. Use it to apply a delegating agent's effective sandbox to a subagent.
// A nil other returns a copy of rs.
func (rs *ResolvedSandbox) Intersect(other *ResolvedSandbox) *ResolvedSandbox {
	if rs == nil {
		return other.DeepCopy()
	}
	out := rs.DeepCopy()
	if other == nil {
		return out
	}
	out.RootFS = rootFSFromRank(min(rootFSRank(out.RootFS, false), rootFSRank(other.RootFS, false)))
	for _, a := range other.Access {
		out.Access = append(out.Access, *a.DeepCopy())
	}
	if execRank(other.Exec.Mode) < execRank(out.Exec.Mode) {
		out.Exec.Mode = other.Exec.Mode
	}
	out.Exec.Allowlists = append(out.Exec.Allowlists, cloneStringLists(other.Exec.Allowlists)...)
	if networkRank(other.Network.Mode) < networkRank(out.Network.Mode) {
		out.Network.Mode = other.Network.Mode
	}
	out.Network.Allowlists = append(out.Network.Allowlists, cloneStringLists(other.Network.Allowlists)...)
	for _, d := range other.Network.Deny {
		if !slices.Contains(out.Network.Deny, d) {
			out.Network.Deny = append(out.Network.Deny, d)
		}
	}
	out.Env.Passthrough = append(out.Env.Passthrough, cloneStringLists(other.Env.Passthrough)...)
	out.Env.Declared = out.Env.Declared || other.Env.Declared
	o := other.Limits
	out.Limits = minSandboxLimits(out.Limits, &o)
	out.Explicit = out.Explicit || other.Explicit
	return out
}

// ─── Selection ──────────────────────────────────────────────────────────────

// sandboxSelection is the outcome of one selection step.
type sandboxSelection struct {
	merged *mergedSandbox
	source SandboxSource
	none   bool
	// unavailable is true when selection fell through to the implicit
	// system/Sandbox/default and it is not registered.
	unavailable bool
}

// selectStep resolves a single selection step value.
func (l sandboxLookup) selectStep(ref, fromNS string, source SandboxSource) (sandboxSelection, error) {
	if ref == SandboxNone {
		return sandboxSelection{none: true, source: source}, nil
	}
	m, err := l.chain(ref, fromNS, map[string]bool{})
	if err != nil {
		if source == SandboxSourceSystem && l.find(KindSandbox, SandboxDefault, "") == nil {
			return sandboxSelection{unavailable: true, source: source}, nil
		}
		return sandboxSelection{}, err
	}
	if !m.sb.Spec.IsEnabled() {
		return sandboxSelection{}, fmt.Errorf("sandbox %q is disabled", m.sb.QualifiedName())
	}
	return sandboxSelection{merged: m, source: source}, nil
}

// selectDefault resolves selection steps 3 and 4: workspace.spec.defaultSandbox,
// then the runtime-provided system/Sandbox/default.
func (l sandboxLookup) selectDefault() (sandboxSelection, error) {
	if ref := l.defaultSandboxRef(); ref != "" {
		return l.selectStep(ref, NamespaceWorkspace, SandboxSourceWorkspace)
	}
	return l.selectStep(SandboxDefault, NamespaceSystem, SandboxSourceSystem)
}

// selectAgent resolves the agent's sandbox selection (agent.spec.sandbox,
// else the workspace/runtime default).
func (l sandboxLookup) selectAgent(agent *Agent) (sandboxSelection, error) {
	if agent != nil && strings.TrimSpace(agent.Spec.Sandbox) != "" {
		return l.selectStep(strings.TrimSpace(agent.Spec.Sandbox), agent.GetNamespace(), SandboxSourceAgent)
	}
	return l.selectDefault()
}

// resolveForAgent computes the effective sandbox for an agent (tool == nil)
// or a tool invocation by that agent.
func (l sandboxLookup) resolveForAgent(agent *Agent, tool *Tool, parent *ResolvedSandbox, strict bool) (*ResolvedSandbox, error) {
	agentSel, agentErr := l.selectAgent(agent)

	var toolSel *sandboxSelection
	if tool != nil && strings.TrimSpace(tool.Spec.Sandbox) != "" {
		ts, err := l.selectStep(strings.TrimSpace(tool.Spec.Sandbox), tool.GetNamespace(), SandboxSourceTool)
		if err != nil {
			return nil, fmt.Errorf("tool %q: %w", tool.QualifiedName(), err)
		}
		toolSel = &ts
	}
	if agentErr != nil {
		return nil, agentErr
	}

	selected := agentSel
	var layers []*mergedSandbox
	if toolSel != nil {
		selected = *toolSel
		if agentSel.merged != nil && (toolSel.merged == nil || agentSel.merged.sb.QualifiedName() != toolSel.merged.sb.QualifiedName()) {
			layers = append(layers, agentSel.merged)
		}
	}
	if selected.merged != nil {
		layers = append(layers, selected.merged)
	}
	return l.finish(selected, layers, parent, strict)
}

// resolveForMCP computes the effective sandbox of a stdio MCP server process.
func (l sandboxLookup) resolveForMCP(mcp *MCP, strict bool) (*ResolvedSandbox, error) {
	if mcp == nil {
		return nil, fmt.Errorf("mcp is nil")
	}
	transport := mcp.Spec.Type
	if transport == "" {
		transport = MCPTransportStdio
	}
	ref := strings.TrimSpace(mcp.Spec.Sandbox)
	if transport != MCPTransportStdio {
		if ref != "" && strict {
			return nil, fmt.Errorf("mcp %q: spec.sandbox is only valid for stdio transport", mcp.QualifiedName())
		}
		return nil, nil
	}
	var sel sandboxSelection
	var err error
	if ref != "" {
		sel, err = l.selectStep(ref, mcp.GetNamespace(), SandboxSourceMCP)
	} else {
		sel, err = l.selectDefault()
	}
	if err != nil {
		return nil, fmt.Errorf("mcp %q: %w", mcp.QualifiedName(), err)
	}
	var layers []*mergedSandbox
	if sel.merged != nil {
		layers = append(layers, sel.merged)
	}
	return l.finish(sel, layers, nil, strict)
}

// finish applies none/unavailable handling, builds the ResolvedSandbox, and
// intersects the delegating agent's sandbox.
func (l sandboxLookup) finish(selected sandboxSelection, layers []*mergedSandbox, parent *ResolvedSandbox, strict bool) (*ResolvedSandbox, error) {
	required := l.required()
	switch {
	case selected.none:
		if required {
			return nil, fmt.Errorf("sandbox %q is not allowed: workspace policies.sandbox.required is true", SandboxNone)
		}
		if parent != nil {
			out := parent.DeepCopy()
			out.Source = SandboxSourceDelegator
			return out, nil
		}
		return nil, nil
	case selected.unavailable:
		if parent != nil {
			out := parent.DeepCopy()
			out.Source = SandboxSourceDelegator
			return out, nil
		}
		if required {
			return nil, fmt.Errorf("workspace policies.sandbox.required is true: %w", ErrNoDefaultSandbox)
		}
		return nil, ErrNoDefaultSandbox
	}
	explicit := selected.source != SandboxSourceSystem || required
	rs, err := l.build(sandboxBuild{
		selected: selected.merged,
		layers:   layers,
		source:   selected.source,
		explicit: explicit,
		strict:   strict,
	})
	if err != nil {
		return nil, err
	}
	if parent != nil {
		rs = rs.Intersect(parent)
	}
	return rs, nil
}

// ─── Public entry points ────────────────────────────────────────────────────

// sandboxScope extracts the base registry and active project slug from a
// Resolver. Only *Registry and *ScopedRegistry are supported.
func sandboxScope(resolver Resolver) (*Registry, string, error) {
	switch r := resolver.(type) {
	case *Registry:
		if r != nil {
			return r, "", nil
		}
	case *ScopedRegistry:
		if r != nil && r.base != nil {
			return r.base, r.projectSlug, nil
		}
	}
	return nil, "", fmt.Errorf("sandbox resolution requires a *Registry or *ScopedRegistry resolver")
}

func withSandboxLookup[T any](resolver Resolver, fn func(l sandboxLookup) (T, error)) (T, error) {
	var zero T
	base, scope, err := sandboxScope(resolver)
	if err != nil {
		return zero, err
	}
	base.mu.RLock()
	defer base.mu.RUnlock()
	return fn(sandboxLookup{r: base, scope: scope})
}

// Sandbox returns the agent's effective sandbox: agent.spec.sandbox (after
// extends merging), else workspace.spec.defaultSandbox, else the
// runtime-provided system/Sandbox/default, with its rules intersected with
// the workspace ceiling and, for subagents, ParentSandbox.
//
// It returns (nil, nil) when the agent selects "none" (and no delegating
// sandbox applies), and ErrNoDefaultSandbox when nothing is selected and
// system/Sandbox/default is not registered.
func (ra *ResolvedAgent) Sandbox() (*ResolvedSandbox, error) {
	if ra == nil || ra.Agent == nil {
		return nil, fmt.Errorf("resolved agent is nil")
	}
	if ra.resolver == nil {
		return nil, fmt.Errorf("resolved agent %q has no resolver; resolve it via ResolveAgent", ra.Agent.Metadata.Name)
	}
	return withSandboxLookup(ra.resolver, func(l sandboxLookup) (*ResolvedSandbox, error) {
		return l.resolveForAgent(ra.Agent, nil, ra.ParentSandbox, true)
	})
}

// SandboxForTool returns the effective sandbox for an invocation of tool by
// this agent. The provider, config, rootfs, mounts, and workdir are taken from
// the most specific selected sandbox (tool.spec.sandbox, agent.spec.sandbox,
// workspace.spec.defaultSandbox, system/Sandbox/default), and the rule
// sections are intersected across the workspace ceiling, the agent's sandbox,
// the tool's sandbox (when different), and ParentSandbox.
//
// It returns (nil, nil) when "none" is selected (and no delegating sandbox
// applies), and ErrNoDefaultSandbox when nothing is selected and
// system/Sandbox/default is not registered.
func (ra *ResolvedAgent) SandboxForTool(tool *Tool) (*ResolvedSandbox, error) {
	if ra == nil || ra.Agent == nil {
		return nil, fmt.Errorf("resolved agent is nil")
	}
	if tool == nil {
		return nil, fmt.Errorf("tool is nil")
	}
	if ra.resolver == nil {
		return nil, fmt.Errorf("resolved agent %q has no resolver; resolve it via ResolveAgent", ra.Agent.Metadata.Name)
	}
	return withSandboxLookup(ra.resolver, func(l sandboxLookup) (*ResolvedSandbox, error) {
		return l.resolveForAgent(ra.Agent, tool, ra.ParentSandbox, true)
	})
}

// ResolveMCPSandbox returns the effective sandbox of a stdio MCP server
// process: mcp.spec.sandbox, else workspace.spec.defaultSandbox, else
// system/Sandbox/default, intersected with the workspace ceiling only (agent
// sandboxes never apply to shared MCP servers). Remote MCP servers return
// (nil, nil).
func (r *Registry) ResolveMCPSandbox(mcp *MCP) (*ResolvedSandbox, error) {
	return withSandboxLookup(r, func(l sandboxLookup) (*ResolvedSandbox, error) {
		return l.resolveForMCP(mcp, true)
	})
}

// ResolveMCPSandbox returns the effective sandbox of a stdio MCP server
// process within this project scope.
func (s *ScopedRegistry) ResolveMCPSandbox(mcp *MCP) (*ResolvedSandbox, error) {
	return withSandboxLookup(s, func(l sandboxLookup) (*ResolvedSandbox, error) {
		return l.resolveForMCP(mcp, true)
	})
}

// ResolveSandbox resolves a Sandbox by reference, merging its extends chain
// and intersecting its rules with the workspace ceiling.
func (r *Registry) ResolveSandbox(ref string) (*ResolvedSandbox, error) {
	return withSandboxLookup(r, func(l sandboxLookup) (*ResolvedSandbox, error) {
		return l.resolveRef(ref)
	})
}

// ResolveSandbox resolves a Sandbox by reference within this project scope,
// merging its extends chain and intersecting its rules with the workspace
// ceiling.
func (s *ScopedRegistry) ResolveSandbox(ref string) (*ResolvedSandbox, error) {
	return withSandboxLookup(s, func(l sandboxLookup) (*ResolvedSandbox, error) {
		return l.resolveRef(ref)
	})
}

func (l sandboxLookup) resolveRef(ref string) (*ResolvedSandbox, error) {
	if strings.TrimSpace(ref) == SandboxNone {
		return nil, fmt.Errorf("%q is a reserved value, not a Sandbox reference", SandboxNone)
	}
	sel, err := l.selectStep(strings.TrimSpace(ref), "", SandboxSourceReference)
	if err != nil {
		return nil, err
	}
	return l.finish(sel, []*mergedSandbox{sel.merged}, nil, true)
}
