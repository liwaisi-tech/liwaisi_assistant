package acl

type Rule struct {
	Binary       string   `yaml:"binary"`
	BinarySHA256 string   `yaml:"binary_sha256"`
	Flags        []string `yaml:"flags"`
	Decision     string   `yaml:"decision"`
	Reason       string   `yaml:"reason"`
}

type ACL struct {
	Rules []Rule `yaml:"rules"`
}

type Decision struct {
	Kind   string
	Reason string
	RuleID string
}

const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
	DecisionHITL  = "hitl"
)
