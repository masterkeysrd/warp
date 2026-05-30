package warp

// Skill is a warp resource that bundles expertise guidelines for a specific
// domain. An agent loads a skill's instructions to adopt its persona or
// follow its conventions.
type Skill struct {
	BaseResource `yaml:",inline"`
	// Spec holds the skill-specific configuration.
	Spec SkillSpec `yaml:"spec"`
}

// DeepCopy returns a deep copy of the Skill.
func (s *Skill) DeepCopy() *Skill {
	if s == nil {
		return nil
	}
	out := new(Skill)
	out.BaseResource = *s.BaseResource.DeepCopy()
	out.Spec = *s.Spec.DeepCopy()
	return out
}

// SkillSpec contains the configuration details for a Skill resource.
type SkillSpec struct {
	// Instructions is the expertise prompt populated from the Markdown body
	// of the file (below the closing front-matter delimiter).
	Instructions string `yaml:"instructions"`
}

// DeepCopy returns a deep copy of the SkillSpec.
func (s *SkillSpec) DeepCopy() *SkillSpec {
	if s == nil {
		return nil
	}
	out := new(SkillSpec)
	*out = *s
	return out
}
