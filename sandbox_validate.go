package warp

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

// sandboxValidator implements the sandbox-related validation rules (Sections
// 7.2, 7.5, 7.10–7.14). It must be used while the registry read lock is held.
type sandboxValidator struct {
	l        sandboxLookup
	opts     *ValidateOptions
	warnings []string
}

func newSandboxValidator(r *Registry, opts *ValidateOptions) *sandboxValidator {
	return &sandboxValidator{l: sandboxLookup{r: r, all: true}, opts: opts}
}

func (v *sandboxValidator) warn(msg string) {
	for _, w := range v.warnings {
		if w == msg {
			return
		}
	}
	v.warnings = append(v.warnings, msg)
}

// checkSandboxRef validates a reference to a Sandbox (or the reserved "none").
func (v *sandboxValidator) checkSandboxRef(label, field, ref, fromNS string, allowNone bool, emit func(string, error)) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return
	}
	if ref == SandboxNone {
		if !allowNone {
			emit(field, fmt.Errorf("%s: %q is not allowed; %s must reference a Sandbox", label, SandboxNone, field))
			return
		}
		if v.l.required() {
			emit(field, fmt.Errorf("%s: sandbox %q is not allowed because workspace policies.sandbox.required is true", label, SandboxNone))
		}
		return
	}
	if err := validateResourceRef(ref); err != nil {
		emit(field, fmt.Errorf("%s: %w", label, err))
		return
	}
	res := v.l.find(KindSandbox, ref, fromNS)
	if res == nil {
		emit(field, fmt.Errorf("%s references missing sandbox: %s", label, ref))
		return
	}
	if sb, ok := res.(*Sandbox); ok && !sb.Spec.IsEnabled() {
		emit(field, fmt.Errorf("%s references disabled sandbox: %s", label, res.QualifiedName()))
	}
}

// checkDriver validates a SandboxProvider or FileSystem driver name against
// the reserved names, the workspace drivers filter, and the driver registry.
// It returns the registered DriverInfo when available.
func (v *sandboxValidator) checkDriver(label, kindLabel, driver string, reserved []string, lookup func(string) (DriverInfo, bool), config map[string]any, emit func(string, error)) (DriverInfo, bool) {
	if err := validateDriverName(kindLabel, driver, reserved); err != nil {
		emit("spec.driver", fmt.Errorf("%s: %w", label, err))
		return DriverInfo{}, false
	}
	if !v.l.policies().AllowsDriver(driver) {
		emit("spec.driver", fmt.Errorf("%s: driver %q is not allowed by workspace policies.sandbox.drivers", label, driver))
	}
	info, ok := lookup(driver)
	if !ok {
		if v.opts.strictDrivers() {
			emit("spec.driver", fmt.Errorf("%s: %s driver %q is not registered", label, kindLabel, driver))
		} else if IsQualifiedDriverName(driver) {
			v.warn(fmt.Sprintf("%s: %s driver %q is not registered; its capability, command, and configuration checks were skipped", label, kindLabel, driver))
		}
		return DriverInfo{}, false
	}
	if info.ValidateConfig != nil {
		if err := info.ValidateConfig(deepCopyConfig(config)); err != nil {
			emit("spec.config", fmt.Errorf("%s: driver %q rejected spec.config: %w", label, driver, err))
		}
	}
	return info, true
}

func validSandboxMode[T ~string](mode T, allowed ...T) bool {
	if mode == "" {
		return true
	}
	for _, a := range allowed {
		if mode == a {
			return true
		}
	}
	return false
}

// checkRuleSections validates the modes, access globs, and (for sandboxes)
// list consistency of the rule sections shared by Sandbox and SandboxPolicies.
func checkRuleSections(label, prefix string, rootfs *SandboxRootFS, access *SandboxAccess, exec *SandboxExec, network *SandboxNetwork, isCeiling bool, emit func(string, error)) {
	if rootfs != nil && !validSandboxMode(rootfs.Mode, SandboxRootFSReadWrite, SandboxRootFSReadOnly, SandboxRootFSNone) {
		emit(prefix+"rootfs.mode", fmt.Errorf("%s: invalid rootfs.mode %q (must be rw, ro, or none)", label, rootfs.Mode))
	}
	if exec != nil {
		if !validSandboxMode(exec.Mode, SandboxExecNone, SandboxExecAllowlist, SandboxExecAny) {
			emit(prefix+"exec.mode", fmt.Errorf("%s: invalid exec.mode %q (must be none, allowlist, or any)", label, exec.Mode))
		}
		if !isCeiling && len(exec.Allow) > 0 && exec.Mode != SandboxExecAllowlist {
			emit(prefix+"exec.allow", fmt.Errorf("%s: exec.allow is set but exec.mode is not allowlist", label))
		}
	}
	if network != nil {
		if !validSandboxMode(network.Mode, SandboxNetworkNone, SandboxNetworkAllowlist, SandboxNetworkOpen) {
			emit(prefix+"network.mode", fmt.Errorf("%s: invalid network.mode %q (must be none, allowlist, or open)", label, network.Mode))
		}
		if !isCeiling && len(network.Allow) > 0 && network.Mode != SandboxNetworkAllowlist {
			emit(prefix+"network.allow", fmt.Errorf("%s: network.allow is set but network.mode is not allowlist", label))
		}
	}
	if access != nil {
		for _, list := range []struct {
			name  string
			globs []string
		}{{"read", access.Read}, {"write", access.Write}, {"deny", access.Deny}} {
			for _, g := range list.globs {
				if !validSandboxGlob(g) {
					emit(prefix+"access."+list.name, fmt.Errorf("%s: invalid access.%s glob %q", label, list.name, g))
				}
			}
		}
	}
}

// ─── Workspace (7.5) ────────────────────────────────────────────────────────

func (v *sandboxValidator) validateWorkspace(def *WorkspaceDef, emit func(string, error)) {
	if ref := strings.TrimSpace(def.Spec.DefaultSandbox); ref != "" {
		v.checkSandboxRef("workspace defaultSandbox", "spec.defaultSandbox", ref, NamespaceWorkspace, false, emit)
	}
	p := v.l.policies()
	if p == nil {
		return
	}
	const label = "workspace policies.sandbox"
	const prefix = "spec.policies.sandbox."
	if !validSandboxMode(p.HostWrite, SandboxHostWriteAllow, SandboxHostWriteDeny) {
		emit(prefix+"hostWrite", fmt.Errorf("%s: invalid hostWrite %q (must be allow or deny)", label, p.HostWrite))
	}
	checkRuleSections(label, prefix, p.RootFS, p.Access, p.Exec, p.Network, true, emit)
	if p.Env != nil && p.Env.Set != nil {
		emit(prefix+"env.set", fmt.Errorf("%s: env.set is not allowed in the workspace ceiling (only passthrough)", label))
	}
	if err := validateResourceFilter(p.Drivers, label+".drivers"); err != nil {
		emit(prefix+"drivers", err)
	}
	for _, hp := range p.HostPaths {
		if !validSandboxGlob(hp) {
			emit(prefix+"hostPaths", fmt.Errorf("%s: invalid hostPaths glob %q", label, hp))
		}
	}
	if p.Required && def.Spec.DefaultSandbox == "" && v.l.find(KindSandbox, SandboxDefault, "") == nil {
		var unresolved []string
		for _, res := range v.sortedResources() {
			switch t := res.(type) {
			case *Tool:
				if len(t.Spec.Command) > 0 && t.Spec.IsEnabled() && strings.TrimSpace(t.Spec.Sandbox) == "" {
					unresolved = append(unresolved, t.QualifiedName())
				}
			case *MCP:
				if (t.Spec.Type == "" || t.Spec.Type == MCPTransportStdio) && t.Spec.IsEnabled() && strings.TrimSpace(t.Spec.Sandbox) == "" {
					unresolved = append(unresolved, t.QualifiedName())
				}
			}
		}
		if len(unresolved) > 0 {
			v.warn(fmt.Sprintf("%s.required is true but no workspace defaultSandbox is set and %s is not registered; %s must be provided by the runtime for: %s", label, SandboxDefault, SandboxDefault, strings.Join(unresolved, ", ")))
		}
	}
}

func (v *sandboxValidator) sortedResources() []Resource {
	keys := make([]string, 0, len(v.l.r.resources))
	for k := range v.l.r.resources {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Resource, 0, len(keys))
	for _, k := range keys {
		out = append(out, v.l.r.resources[k])
	}
	return out
}

// ─── Agent (7.2) ────────────────────────────────────────────────────────────

func (v *sandboxValidator) validateAgent(name string, agent *Agent, emit func(string, error)) {
	label := fmt.Sprintf("agent %s", name)
	v.checkSandboxRef(label, "spec.sandbox", agent.Spec.Sandbox, agent.GetNamespace(), true, emit)
	if agent.Spec.Policies != nil && agent.Spec.Policies.Sandbox != nil {
		emit("spec.policies.sandbox", fmt.Errorf("%s: policies.sandbox is only supported on the Workspace; use spec.sandbox to narrow an agent's sandbox", label))
	}
}

// mergedAgentSandbox walks an agent's extends chain and returns a shallow
// view of the agent with the effective sandbox and the union of tool refs.
func (v *sandboxValidator) mergedAgentSandbox(agent *Agent) *Agent {
	out := &Agent{BaseResource: agent.BaseResource}
	visited := map[string]bool{}
	curr := agent
	for curr != nil && !visited[curr.QualifiedName()] {
		visited[curr.QualifiedName()] = true
		if out.Spec.Sandbox == "" {
			out.Spec.Sandbox = curr.Spec.Sandbox
		}
		out.Spec.Tools = append(out.Spec.Tools, curr.Spec.Tools...)
		if curr.Spec.Extends == "" {
			break
		}
		curr, _ = v.l.find(KindAgent, curr.Spec.Extends, curr.GetNamespace()).(*Agent)
	}
	return out
}

func (v *sandboxValidator) agentReferencesTool(agent *Agent, tool *Tool) bool {
	nsName := tool.GetNamespace() + "/" + tool.GetName()
	for _, ref := range agent.Spec.Tools {
		if ref == "*" {
			return true
		}
		if hasGlobMeta(ref) {
			if matchResourcePattern(ref, tool.GetName(), nsName, tool.QualifiedName()) {
				return true
			}
			continue
		}
		if res := v.l.find(KindTool, ref, agent.GetNamespace()); res != nil && res.QualifiedName() == tool.QualifiedName() {
			return true
		}
	}
	return false
}

// ─── Tool (7.11) ────────────────────────────────────────────────────────────

func (v *sandboxValidator) validateTool(name string, tool *Tool, emit func(string, error)) {
	label := fmt.Sprintf("tool %s", name)
	v.checkSandboxRef(label, "spec.sandbox", tool.Spec.Sandbox, tool.GetNamespace(), true, emit)
	if len(tool.Spec.Command) == 0 || !tool.Spec.IsEnabled() {
		return
	}
	seen := map[string]bool{}
	check := func(agent *Agent) {
		rs, err := v.l.resolveForAgent(agent, tool, nil, false)
		if err != nil || rs == nil {
			return
		}
		for _, msg := range v.executableIssues(label, tool.Spec.Command[0], rs) {
			if !seen[msg] {
				seen[msg] = true
				emit("spec.command", errors.New(msg))
			}
		}
	}
	// Generic context. A tool that selects its own sandbox is checked against
	// that sandbox and the workspace ceiling; otherwise against the
	// workspace/runtime default.
	if ref := strings.TrimSpace(tool.Spec.Sandbox); ref != "" && ref != SandboxNone {
		check(&Agent{BaseResource: BaseResource{Kind: KindAgent, Namespace: tool.GetNamespace()}, Spec: AgentSpec{Sandbox: SandboxNone}})
	} else {
		check(nil)
	}
	// Every agent referencing this tool, intersected with the agent's sandbox.
	for _, res := range v.sortedResources() {
		agent, ok := res.(*Agent)
		if !ok {
			continue
		}
		merged := v.mergedAgentSandbox(agent)
		if strings.TrimSpace(merged.Spec.Sandbox) == SandboxNone {
			continue
		}
		if v.agentReferencesTool(merged, tool) {
			check(merged)
		}
	}
}

func sandboxLabel(rs *ResolvedSandbox) string {
	if rs.Sandbox != nil {
		return rs.Sandbox.QualifiedName()
	}
	return "(unnamed)"
}

func isTemplatedCommand(s string) bool {
	return strings.Contains(s, "$") || strings.Contains(s, "{{")
}

// executableIssues reports why a command cannot run in the effective sandbox.
func (v *sandboxValidator) executableIssues(label, cmd0 string, rs *ResolvedSandbox) []string {
	var issues []string
	sbName := sandboxLabel(rs)
	if rs.Exec.Mode == SandboxExecNone {
		issues = append(issues, fmt.Sprintf("%s: spec.command cannot run in sandbox %s: exec.mode is none", label, sbName))
	}
	info, registered := v.opts.sandboxDriver(rs.Driver)
	if registered && !info.HasCapability(SandboxCapabilityExec) {
		issues = append(issues, fmt.Sprintf("%s: spec.command cannot run in sandbox %s: driver %q lacks the %s capability", label, sbName, rs.Driver, SandboxCapabilityExec))
	}
	if isTemplatedCommand(cmd0) {
		return issues
	}
	if rs.Exec.Mode == SandboxExecAllowlist && !rs.AllowsCommand(cmd0) {
		issues = append(issues, fmt.Sprintf("%s: command %q is not allowed by the exec allowlist of sandbox %s", label, cmd0, sbName))
	}
	if registered && len(info.Commands) > 0 && !commandInAllowlist(info.Commands, cmd0) {
		issues = append(issues, fmt.Sprintf("%s: command %q is not offered by sandbox driver %q", label, cmd0, rs.Driver))
	}
	return issues
}

// ─── MCP (7.10) ─────────────────────────────────────────────────────────────

func (v *sandboxValidator) validateMCP(name string, mcp *MCP, emit func(string, error)) {
	label := fmt.Sprintf("mcp %s", name)
	transport := mcp.Spec.Type
	if transport == "" {
		transport = MCPTransportStdio
	}
	if ref := strings.TrimSpace(mcp.Spec.Sandbox); ref != "" && transport != MCPTransportStdio {
		emit("spec.sandbox", fmt.Errorf("%s: spec.sandbox is only valid for stdio transport (type is %q)", label, transport))
	} else {
		v.checkSandboxRef(label, "spec.sandbox", mcp.Spec.Sandbox, mcp.GetNamespace(), true, emit)
	}
	if transport != MCPTransportStdio || len(mcp.Spec.Command) == 0 || !mcp.Spec.IsEnabled() {
		return
	}
	rs, err := v.l.resolveForMCP(mcp, false)
	if err != nil || rs == nil {
		return
	}
	sbName := sandboxLabel(rs)
	if rs.Exec.Mode == SandboxExecNone {
		emit("spec.command", fmt.Errorf("%s: server command cannot run in sandbox %s: exec.mode is none", label, sbName))
	}
	if info, ok := v.opts.sandboxDriver(rs.Driver); ok && !info.HasCapability(SandboxCapabilityExec) {
		emit("spec.command", fmt.Errorf("%s: server command cannot run in sandbox %s: driver %q lacks the %s capability", label, sbName, rs.Driver, SandboxCapabilityExec))
	}
}

// ─── SandboxProvider (7.12) ─────────────────────────────────────────────────

func (v *sandboxValidator) validateSandboxProvider(name string, sp *SandboxProvider, emit func(string, error)) {
	label := fmt.Sprintf("sandbox provider %s", name)
	v.checkDriver(label, "sandbox", sp.Spec.Driver, ReservedSandboxDrivers, v.opts.sandboxDriver, sp.Spec.Config, emit)
	if sp.Spec.Auth != nil {
		if err := sp.Spec.Auth.ValidateWithOptions(v.opts); err != nil {
			emit("spec.auth", fmt.Errorf("%s: %w", label, err))
		}
	}
}

// ─── FileSystem (7.14) ──────────────────────────────────────────────────────

func (v *sandboxValidator) validateFileSystem(name string, fsr *FileSystem, emit func(string, error)) {
	label := fmt.Sprintf("filesystem %s", name)
	driver := fsr.Spec.Driver
	v.checkDriver(label, "filesystem", driver, ReservedFileSystemDrivers, v.opts.fileSystemDriver, fsr.Spec.Config, emit)

	switch fsr.Spec.Persistence {
	case "", FileSystemPersistenceEphemeral, FileSystemPersistencePersistent:
		if driver == FileSystemDriverHost && fsr.Spec.Persistence == FileSystemPersistenceEphemeral {
			emit("spec.persistence", fmt.Errorf("%s: host filesystems cannot be ephemeral", label))
		}
		if driver == FileSystemDriverMemory && fsr.Spec.Persistence == FileSystemPersistencePersistent {
			emit("spec.persistence", fmt.Errorf("%s: memory filesystems cannot be persistent", label))
		}
	default:
		emit("spec.persistence", fmt.Errorf("%s: invalid persistence %q (must be ephemeral or persistent)", label, fsr.Spec.Persistence))
	}

	requireKey := func(key string) bool {
		if fsr.Spec.ConfigString(key) == "" {
			emit("spec.config", fmt.Errorf("%s: %s filesystems require a string config.%s", label, driver, key))
			return false
		}
		return true
	}
	switch driver {
	case FileSystemDriverHost:
		if requireKey("path") && v.l.hostPathsApply() {
			for _, pd := range v.l.projectDirs(fsr.GetNamespace()) {
				hp, _, ok := v.l.hostSource(fsr, pd)
				if !ok {
					emit("spec.config", fmt.Errorf("%s: config.path %q cannot be rendered to verify workspace policies.sandbox.hostPaths", label, fsr.Spec.ConfigString("path")))
					break
				}
				if within, _ := v.l.withinHostPaths(hp, pd); !within {
					emit("spec.config", fmt.Errorf("%s: host path %q is outside workspace policies.sandbox.hostPaths", label, hp))
					break
				}
			}
		}
	case FileSystemDriverOverlay:
		if requireKey("source") {
			v.checkOverlaySource(label, fsr, emit)
		}
	case FileSystemDriverGit:
		requireKey("url")
	}
	if fsr.Spec.Auth != nil {
		if err := fsr.Spec.Auth.ValidateWithOptions(v.opts); err != nil {
			emit("spec.auth", fmt.Errorf("%s: %w", label, err))
		}
	}
}

func (v *sandboxValidator) checkOverlaySource(label string, fsr *FileSystem, emit func(string, error)) {
	visited := map[string]bool{fsr.QualifiedName(): true}
	curr := fsr
	for {
		src := curr.Spec.ConfigString("source")
		if curr == fsr {
			if err := validateResourceRef(src); err != nil {
				emit("spec.config", fmt.Errorf("%s: overlay config.source: %w", label, err))
				return
			}
		}
		next, _ := v.l.find(KindFileSystem, src, curr.GetNamespace()).(*FileSystem)
		if next == nil {
			if curr == fsr {
				emit("spec.config", fmt.Errorf("%s: overlay config.source references missing filesystem: %s", label, src))
			}
			return
		}
		if visited[next.QualifiedName()] {
			emit("spec.config", fmt.Errorf("%s: circular overlay sources detected at %q", label, next.QualifiedName()))
			return
		}
		visited[next.QualifiedName()] = true
		if next.Spec.Driver != FileSystemDriverOverlay {
			return
		}
		curr = next
	}
}

// ─── Sandbox (7.13) ─────────────────────────────────────────────────────────

func (v *sandboxValidator) validateSandbox(name string, sb *Sandbox, emit func(string, error)) {
	label := fmt.Sprintf("sandbox %s", name)
	ns := sb.GetNamespace()
	ceiling := v.l.policies()

	merged := &mergedSandbox{sb: sb.DeepCopy(), providerNS: ns}
	for range sb.Spec.Mounts {
		merged.mountNS = append(merged.mountNS, ns)
	}
	if sb.Spec.Extends != "" {
		if err := validateResourceRef(sb.Spec.Extends); err != nil {
			emit("spec.extends", fmt.Errorf("%s extends: %w", label, err))
		} else if v.l.find(KindSandbox, sb.Spec.Extends, ns) == nil {
			emit("spec.extends", fmt.Errorf("%s extends missing sandbox: %s", label, sb.Spec.Extends))
		} else if m, err := v.l.chain(sb.QualifiedName(), ns, map[string]bool{}); err != nil {
			emit("spec.extends", fmt.Errorf("%s: %w", label, err))
		} else {
			merged = m
		}
	}
	spec := &merged.sb.Spec

	// Provider resolves.
	var provider *SandboxProvider
	if ref := spec.Provider; ref != "" {
		if err := validateResourceRef(ref); err != nil {
			emit("spec.provider", fmt.Errorf("%s: %w", label, err))
		} else {
			provider, _ = v.l.find(KindSandboxProvider, ref, merged.providerNS).(*SandboxProvider)
			switch {
			case provider == nil:
				emit("spec.provider", fmt.Errorf("%s references missing sandbox provider: %s", label, ref))
			case !provider.Spec.IsEnabled():
				emit("spec.provider", fmt.Errorf("%s references disabled sandbox provider: %s", label, provider.QualifiedName()))
			}
		}
	} else if sb.QualifiedName() != SandboxDefault {
		if def, err := v.l.chain(SandboxDefault, NamespaceSystem, map[string]bool{}); err == nil && def.sb.Spec.Provider != "" {
			provider, _ = v.l.find(KindSandboxProvider, def.sb.Spec.Provider, def.providerNS).(*SandboxProvider)
		}
	}
	var driver string
	if provider != nil {
		driver = provider.Spec.Driver
	}
	info, registered := v.opts.sandboxDriver(driver)

	// Own mounts: references, paths, modes, duplicates.
	seenPaths := map[string]bool{}
	for i, m := range sb.Spec.Mounts {
		field := fmt.Sprintf("spec.mounts[%d]", i)
		var fsr *FileSystem
		if fsRef := strings.TrimSpace(m.FileSystem); fsRef == "" {
			emit(field+".filesystem", fmt.Errorf("%s: mounts[%d].filesystem is required", label, i))
		} else if err := validateResourceRef(fsRef); err != nil {
			emit(field+".filesystem", fmt.Errorf("%s: %w", label, err))
		} else if fsr, _ = v.l.find(KindFileSystem, fsRef, ns).(*FileSystem); fsr == nil {
			emit(field+".filesystem", fmt.Errorf("%s references missing filesystem: %s", label, m.FileSystem))
		} else if !fsr.Spec.IsEnabled() {
			emit(field+".filesystem", fmt.Errorf("%s references disabled filesystem: %s", label, fsr.QualifiedName()))
		}
		var hostPath string
		hostBacked := false
		if fsr != nil {
			var ok bool
			hostPath, hostBacked, ok = v.l.hostSource(fsr, firstOr(v.l.projectDirs(fsr.GetNamespace()), ""))
			if !ok {
				hostPath = ""
			}
		}
		key := ""
		switch {
		case m.Path != "":
			if !path.IsAbs(m.Path) {
				emit(field+".path", fmt.Errorf("%s: mount path %q must be absolute", label, m.Path))
			}
			key = path.Clean(m.Path)
		case fsr != nil && !hostBacked:
			emit(field+".path", fmt.Errorf("%s: mount of filesystem %s requires a path (only host-backed filesystems default to their host path)", label, fsr.QualifiedName()))
		default:
			key = hostPath
		}
		if key != "" {
			if seenPaths[key] {
				emit(field+".path", fmt.Errorf("%s: duplicate mount path %q", label, key))
			}
			seenPaths[key] = true
		}
		if !validSandboxMode(m.Mode, SandboxMountReadOnly, SandboxMountReadWrite) {
			emit(field+".mode", fmt.Errorf("%s: invalid mount mode %q (must be ro or rw)", label, m.Mode))
		}
	}

	// Effective (merged) mounts: write permissions and remapping.
	var mountPaths []string
	for i, m := range spec.Mounts {
		mns := ns
		if i < len(merged.mountNS) {
			mns = merged.mountNS[i]
		}
		fsr, _ := v.l.find(KindFileSystem, m.FileSystem, mns).(*FileSystem)
		if fsr == nil {
			if m.Path != "" {
				mountPaths = append(mountPaths, path.Clean(m.Path))
			}
			continue
		}
		hostPath, hostBacked, hok := v.l.hostSource(fsr, firstOr(v.l.projectDirs(fsr.GetNamespace()), ""))
		effPath := ""
		switch {
		case m.Path != "":
			effPath = path.Clean(m.Path)
		case hostBacked && hok:
			effPath = hostPath
		}
		if effPath != "" {
			mountPaths = append(mountPaths, effPath)
		}
		if m.Mode == SandboxMountReadWrite {
			if fsr.Spec.ReadOnly {
				emit("spec.mounts", fmt.Errorf("%s: filesystem %s is readOnly and cannot be mounted rw", label, fsr.QualifiedName()))
			}
			if fsInfo, ok := v.opts.fileSystemDriver(fsr.Spec.Driver); ok && !fsInfo.HasCapability(FileSystemCapabilityWrite) {
				emit("spec.mounts", fmt.Errorf("%s: filesystem %s driver %q lacks the %s capability and cannot be mounted rw", label, fsr.QualifiedName(), fsr.Spec.Driver, FileSystemCapabilityWrite))
			}
			if fsr.Spec.Driver == FileSystemDriverHost && ceiling != nil && ceiling.HostWrite == SandboxHostWriteDeny {
				emit("spec.mounts", fmt.Errorf("%s: host filesystem %s cannot be mounted rw because workspace policies.sandbox.hostWrite is deny (use an overlay filesystem)", label, fsr.QualifiedName()))
			}
		}
		if registered && hostBacked && hok && m.Path != "" && path.Clean(m.Path) != hostPath && !info.HasCapability(SandboxCapabilityMountsRemap) {
			emit("spec.mounts", fmt.Errorf("%s: mount path %q differs from host path %q but driver %q lacks the %s capability", label, m.Path, hostPath, driver, SandboxCapabilityMountsRemap))
		}
	}

	// Workdir.
	if wd := spec.Workdir; wd != "" {
		if !path.IsAbs(wd) {
			emit("spec.workdir", fmt.Errorf("%s: workdir %q must be absolute", label, wd))
		} else if spec.RootFS != nil && spec.RootFS.Mode == SandboxRootFSNone {
			within := false
			for _, mp := range mountPaths {
				if pathWithin(path.Clean(wd), mp) {
					within = true
					break
				}
			}
			if !within {
				emit("spec.workdir", fmt.Errorf("%s: workdir %q is outside every mount while rootfs.mode is none", label, wd))
			}
		}
	}

	// Modes, lists, and access globs (own spec).
	checkRuleSections(label, "spec.", sb.Spec.RootFS, sb.Spec.Access, sb.Spec.Exec, sb.Spec.Network, false, emit)

	// env.set values.
	if sb.Spec.Env != nil {
		keys := make([]string, 0, len(sb.Spec.Env.Set))
		for k := range sb.Spec.Env.Set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := ValidateStringWithExpressions(sb.Spec.Env.Set[k], v.opts); err != nil {
				emit("spec.env.set", fmt.Errorf("%s: env.set.%s: %w", label, k, err))
			}
		}
	}

	// Capabilities and merged driver config (registered drivers only).
	if registered {
		if rs, err := v.l.build(sandboxBuild{selected: merged, layers: []*mergedSandbox{merged}}); err == nil {
			if missing := info.MissingCapabilities(rs.RequiredCapabilities(false)); len(missing) > 0 {
				emit("spec", fmt.Errorf("%s: driver %q lacks capabilities required by the sandbox: %s", label, driver, strings.Join(missing, ", ")))
			}
			if info.ValidateConfig != nil && len(sb.Spec.Config) > 0 {
				if err := info.ValidateConfig(deepCopyConfig(rs.Config)); err != nil {
					emit("spec.config", fmt.Errorf("%s: driver %q rejected the merged config: %w", label, driver, err))
				}
			}
		}
	}
}
