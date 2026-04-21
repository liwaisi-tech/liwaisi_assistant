package acl

import "fmt"

// Match returns the first matching rule's decision. Matching requires the
// binary_sha256 to be identical AND the rule's Flags to either be empty
// (wildcard) or form a prefix of argv flags. First match wins.
func (a *ACL) Match(binarySHA256 string, flags []string) (Decision, bool) {
	if a == nil {
		return Decision{}, false
	}
	for i, r := range a.Rules {
		if r.BinarySHA256 != binarySHA256 {
			continue
		}
		if !flagsPrefixMatch(r.Flags, flags) {
			continue
		}
		return Decision{
			Kind:   r.Decision,
			Reason: r.Reason,
			RuleID: fmt.Sprintf("%s#%d", r.Binary, i),
		}, true
	}
	return Decision{}, false
}

func flagsPrefixMatch(ruleFlags, argvFlags []string) bool {
	if len(ruleFlags) == 0 {
		return true
	}
	if len(ruleFlags) > len(argvFlags) {
		return false
	}
	for i, f := range ruleFlags {
		if argvFlags[i] != f {
			return false
		}
	}
	return true
}
