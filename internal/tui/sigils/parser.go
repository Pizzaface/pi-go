// Package sigils parses and renders extension-contributed sigils of the
// form [[prefix:id]] in chat content.
package sigils

import "regexp"

// Match is one sigil occurrence.
type Match struct {
	Prefix string
	ID     string
	Raw    string
	Start  int
	End    int
}

var sigilRE = regexp.MustCompile(`\[\[([a-z][a-z0-9-]*):([^\s\]]+)\]\]`)
var prefixRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Parse scans s and returns every match in source order.
func Parse(s string) []Match {
	idxs := sigilRE.FindAllStringSubmatchIndex(s, -1)
	out := make([]Match, 0, len(idxs))
	for _, m := range idxs {
		out = append(out, Match{
			Prefix: s[m[2]:m[3]],
			ID:     s[m[4]:m[5]],
			Raw:    s[m[0]:m[1]],
			Start:  m[0],
			End:    m[1],
		})
	}
	return out
}

// ValidPrefix reports whether prefix matches the syntax extensions may register.
func ValidPrefix(prefix string) bool {
	return prefixRE.MatchString(prefix)
}
