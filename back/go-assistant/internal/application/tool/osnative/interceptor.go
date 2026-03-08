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
	// Block sudo, su, and any attempts to escalate privileges
	if command == "sudo" || command == "su" {
		return fmt.Errorf("security violation: privilege escalation commands (%s) are not permitted", command)
	}

	for _, arg := range args {
		if arg == "sudo" || arg == "su" {
			return fmt.Errorf("security violation: privilege escalation found in arguments (%s)", arg)
		}
	}

	// For potentially destructive or sensitive commands, enforce path boundary
	dangerousCommands := map[string]bool{
		"rm": true, "mv": true, "cp": true, "chmod": true, "chown": true, "chgrp": true,
	}

	if dangerousCommands[command] {
		for _, arg := range args {
			// Skip flags
			if strings.HasPrefix(arg, "-") {
				continue
			}

			// Resolve absolute path and ensure it's within the workspace
			targetPath := arg
			if !filepath.IsAbs(targetPath) {
				// We assume execution happens in workspaceRoot for these checks,
				// or we enforce that targetPath resolves inside workspaceRoot.
				targetPath = filepath.Join(i.workspaceRoot, targetPath)
			}
			targetPath = filepath.Clean(targetPath)

			if !strings.HasPrefix(targetPath, i.workspaceRoot) {
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
