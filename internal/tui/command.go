package tui

import "strings"

// resolveCommand looks up a view constructor by name or alias. Matching is
// case-insensitive and ignores surrounding whitespace and a leading colon.
func resolveCommand(name string, registry map[string]func() View) (View, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	key = strings.TrimPrefix(key, ":")
	build, ok := registry[key]
	if !ok {
		return nil, false
	}
	return build(), true
}
