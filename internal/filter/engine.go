package filter

import "strings"

// Engine applies filter rules to containers and resources during translation.
// A nil or default-constructed Engine is safe to use and acts as a pass-through
// (nothing is skipped or replaced).
type Engine struct {
	cfg *Config
}

// New creates an Engine from the given Config.
// If cfg is nil, a no-op engine is returned.
func New(cfg *Config) *Engine {
	if cfg == nil {
		cfg = &Config{}
	}
	return &Engine{cfg: cfg}
}

// ShouldSkipContainer returns true when a regular container should be excluded
// from translation. reason is a human-readable explanation for log output.
func (e *Engine) ShouldSkipContainer(name, image string) (skip bool, reason string) {
	for _, m := range e.cfg.Containers.Skip {
		if matchesContainer(m, name, image) {
			return true, formatReason("containers.skip", m)
		}
	}
	return false, ""
}

// ShouldSkipInitContainer returns true when an init container should be
// excluded (init containers are never translated, but this lets callers log
// the explicit rule that caused the skip).
func (e *Engine) ShouldSkipInitContainer(name, image string) (skip bool, reason string) {
	for _, m := range e.cfg.InitContainers.Skip {
		if matchesContainer(m, name, image) {
			return true, formatReason("initContainers.skip", m)
		}
	}
	// Init containers are always skipped even without an explicit rule.
	return true, "initContainers are not translatable to compose services"
}

// FindReplacement returns the first ContainerReplacement whose Match clause
// matches the given container name and image. Returns nil if none matches.
func (e *Engine) FindReplacement(name, image string) *ContainerReplacement {
	for i := range e.cfg.Containers.Replace {
		r := &e.cfg.Containers.Replace[i]
		if matchesContainer(r.Match, name, image) {
			return r
		}
	}
	return nil
}

// ShouldSkipResource returns true when a K8s resource (identified by kind and
// metadata.name) should be excluded from translation.
func (e *Engine) ShouldSkipResource(kind, name string) (skip bool, reason string) {
	for _, m := range e.cfg.Resources.Skip {
		kindMatch := m.Kind == "" || globMatch(m.Kind, kind)
		nameMatch := m.Name == "" || globMatch(m.Name, name)
		if kindMatch && nameMatch {
			return true, formatReason("resources.skip", m)
		}
	}
	return false, ""
}

// --- helpers -----------------------------------------------------------------

func formatReason(section string, m interface{}) string {
	switch v := m.(type) {
	case ContainerMatcher:
		parts := []string{}
		if v.Name != "" {
			parts = append(parts, "name="+v.Name)
		}
		if v.Image != "" {
			parts = append(parts, "image="+v.Image)
		}
		return section + " matched " + strings.Join(parts, ", ")
	case ResourceMatcher:
		parts := []string{}
		if v.Kind != "" {
			parts = append(parts, "kind="+v.Kind)
		}
		if v.Name != "" {
			parts = append(parts, "name="+v.Name)
		}
		return section + " matched " + strings.Join(parts, ", ")
	}
	return section
}

// matchesContainer checks whether a ContainerMatcher applies to the given
// container name and image.
//
// Matching semantics:
//   - If both Name and Image are non-empty, BOTH must match.
//   - If only Name is set, Name must match (image is not checked).
//   - If only Image is set, Image must match (name is not checked).
//   - If both are empty the matcher never matches (safety guard).
func matchesContainer(m ContainerMatcher, name, image string) bool {
	switch {
	case m.Name == "" && m.Image == "":
		return false // empty matcher never matches
	case m.Name != "" && m.Image != "":
		return globMatch(m.Name, name) && globMatch(m.Image, image)
	case m.Name != "":
		return globMatch(m.Name, name)
	default:
		return globMatch(m.Image, image)
	}
}

// globMatch reports whether pattern matches s.
// The pattern syntax is similar to shell globbing:
//   - * matches any sequence of characters, including path separators (/)
//   - ? matches any single character
//   - all other characters match literally (case-sensitive)
func globMatch(pattern, s string) bool {
	// Fast path: no wildcards — exact match.
	if !strings.ContainsAny(pattern, "*?") {
		return pattern == s
	}
	return matchGlob(pattern, s)
}

// matchGlob is a simple recursive glob matcher where * crosses path separators.
func matchGlob(pattern, s string) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			// Consume consecutive stars.
			for len(pattern) > 0 && pattern[0] == '*' {
				pattern = pattern[1:]
			}
			if len(pattern) == 0 {
				return true // trailing star(s) match everything
			}
			// Try matching the rest of the pattern at every position in s.
			for i := 0; i <= len(s); i++ {
				if matchGlob(pattern, s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
			pattern = pattern[1:]
			s = s[1:]
		default:
			if len(s) == 0 || pattern[0] != s[0] {
				return false
			}
			pattern = pattern[1:]
			s = s[1:]
		}
	}
	return len(s) == 0
}
