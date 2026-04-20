package tools

// shell_parse.go: the parser moved to the cpn package so fire_llm.go can
// use it without cycling through cpn/tools (which already imports cpn).
// This stub remains to avoid a stale file during the migration; consumers
// should call cpn.ParseShellInvocation directly.
