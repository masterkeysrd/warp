package warp

import (
	"fmt"
	"strings"
)

// Resource is the common interface satisfied by every warp resource value
// stored in the registry. All concrete types (*Agent, *Skill, *Command, etc.)
// satisfy this interface through their embedded BaseResource.
type Resource interface {
	// GetKind returns the resource's Kind (e.g. KindSkill).
	GetKind() Kind
	// GetName returns the short metadata name declared in the YAML front-matter.
	GetName() string
	// GetNamespace returns the namespace this resource was loaded into. Empty
	// when the resource was loaded via the legacy filesystem path and has not
	// been assigned to a namespace.
	GetNamespace() string
	// QualifiedName returns the "namespace/Kind/name" key that uniquely
	// identifies this resource in the registry.
	QualifiedName() string
	// GetMetadata returns the resource's Metadata block.
	GetMetadata() Metadata
}

// Standard namespace identifiers. These are reserved; plugins must not use them.
const (
	// NamespaceLocal is the highest-priority namespace, corresponding to
	// project-local .agents/ resources.
	NamespaceLocal = "local"

	// NamespaceWorkspace corresponds to workspace-global .agents/ resources
	// (only populated when projects are sub-directories).
	NamespaceWorkspace = "workspace"

	// NamespaceUser corresponds to user-level configuration resources.
	NamespaceUser = "user"

	// NamespaceSystem corresponds to embedded builtin resources shipped with
	// the runtime.
	NamespaceSystem = "system"
)

// namespacePriority maps well-known namespace names to their numeric priority.
// Higher values win over lower ones during shadowing. Unknown namespaces get 0.
var namespacePriority = map[string]int{
	NamespaceLocal:     100,
	NamespaceWorkspace: 80,
	NamespaceUser:      60,
	NamespaceSystem:    40,
}

// NamespacePriority returns the numeric priority for a namespace name.
// Unknown namespaces return 0.
func NamespacePriority(ns string) int {
	if p, ok := namespacePriority[ns]; ok {
		return p
	}
	return 0
}

// MakeQualifiedName constructs the "namespace/Kind/name" qualified name.
func MakeQualifiedName(namespace string, kind Kind, name string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, kind, name)
}

// ParseQualifiedName splits a qualified name of the form "namespace/Kind/name"
// into its three components. Returns ok=false when the input does not contain
// exactly two "/" separators.
func ParseQualifiedName(qn string) (namespace string, kind Kind, name string, ok bool) {
	parts := strings.SplitN(qn, "/", 3)
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], Kind(parts[1]), parts[2], true
}
