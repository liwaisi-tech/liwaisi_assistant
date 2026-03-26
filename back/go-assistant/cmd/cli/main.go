// Package main is the entry point for the liwaisi CLI.
package main

import (
	"fmt"
	"os"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version.String())
		return
	}

	fmt.Println("liwaisi: agentic CPN engine")
	fmt.Println("use --version for build info")
}
