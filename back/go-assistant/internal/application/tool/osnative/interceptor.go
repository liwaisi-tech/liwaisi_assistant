package osnative

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// SecurityInterceptor checks if a command is allowed to be run by the OS-native agent.
type SecurityInterceptor struct {
	workspaceRoot string
}

// NewSecurityInterceptor creates a new SecurityInterceptor bound to the given workspace.
func NewSecurityInterceptor(workspaceRoot string) *SecurityInterceptor {
	return &SecurityInterceptor{
		workspaceRoot: filepath.Clean(workspaceRoot),
	}
}

// Check validates the command and its arguments against security policies.
// It blocks commands attempting privilege escalation (sudo, su) or
// destructive actions outside the workspace.
func (i *SecurityInterceptor) Check(command string, args []string) error {
	baseCmd := filepath.Base(command)
	escalators := map[string]bool{
		"sudo": true, "su": true, "pkexec": true, "doas": true,
	}

	// Block sudo, su, and any attempts to escalate privileges
	if escalators[baseCmd] {
		return fmt.Errorf("security violation: privilege escalation commands (%s) are not permitted", command)
	}

	for _, arg := range args {
		if escalators[filepath.Base(arg)] {
			return fmt.Errorf("security violation: privilege escalation found in arguments (%s)", arg)
		}
	}

	// Define a strict allowlist of permitted OS commands.
	allowlist := map[string]bool{
		"grep": true, "awk": true, "sed": true, "find": true, "ls": true, "cat": true,
		"head": true, "tail": true, "wc": true, "ps": true, "ping": true, "curl": true,
		"diff": true, "du": true, "df": true, "free": true, "uptime": true, "whoami": true,
		"id": true, "date": true, "tar": true, "gzip": true, "gunzip": true, "zip": true,
		"unzip": true, "tree": true, "stat": true, "file": true, "sort": true, "uniq": true,
		"xargs": true, "echo": true, "printenv": true, "env": true, "mkdir": true,
		"rm": true, "mv": true, "cp": true, "chmod": true, "chown": true, "chgrp": true,
		"touch": true, "jq": true, "base64": true,
	}

	if !allowlist[baseCmd] {
		return fmt.Errorf("security violation: command %s is not in the allowlist of permitted OS commands", command)
	}

	// Check path boundaries for all arguments
	for _, arg := range args {
		valToCheck := arg
		if strings.HasPrefix(arg, "-") && strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			if len(parts) == 2 {
				valToCheck = parts[1]
			}
		}

		// Only attempt to validate boundary if it looks like a path navigating outside
		if strings.HasPrefix(valToCheck, "/") || strings.Contains(valToCheck, "../") || strings.Contains(valToCheck, "..\\") {
			targetPath := valToCheck
			if !filepath.IsAbs(targetPath) {
				targetPath = filepath.Join(i.workspaceRoot, targetPath)
			}
			targetPath = filepath.Clean(targetPath)

			rel, err := filepath.Rel(i.workspaceRoot, targetPath)
			if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
				return fmt.Errorf("security violation: command %s attempts to access path %s outside workspace bounds", command, arg)
			}
		}
	}

	return nil
}

// Execute runs the command if it passes the security check, otherwise returns an error.
func (i *SecurityInterceptor) Execute(command string, args []string, cwDir string) ([]byte, error) {
	if err := i.Check(command, args); err != nil {
		return nil, err
	}

	cmd := exec.Command(command, args...)
	if cwDir != "" {
		cmd.Dir = cwDir
	} else {
		cmd.Dir = i.workspaceRoot
	}

	return cmd.CombinedOutput()
}
