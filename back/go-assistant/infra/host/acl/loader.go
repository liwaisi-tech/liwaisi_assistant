package acl

import (
	"bytes"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

var sha256HexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func LoadACL(path string) (*ACL, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("acl: read %s: %w", path, err)
	}
	return LoadACLBytes(b)
}

func LoadACLBytes(b []byte) (*ACL, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var a ACL
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("acl: decode: %w", err)
	}
	if err := validate(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

func validate(a *ACL) error {
	for i, r := range a.Rules {
		if r.BinarySHA256 == "" {
			return fmt.Errorf("acl: rule %d missing binary_sha256", i)
		}
		if !sha256HexRe.MatchString(r.BinarySHA256) {
			return fmt.Errorf("acl: rule %d invalid binary_sha256 (want 64 lowercase hex chars): %q", i, r.BinarySHA256)
		}
		switch r.Decision {
		case DecisionAllow, DecisionDeny, DecisionHITL:
		default:
			return fmt.Errorf("acl: rule %d invalid decision %q (want allow|deny|hitl)", i, r.Decision)
		}
	}
	return nil
}
