package main

// os_seam.go — thin os/* shims so tests can stub file/hostname lookups.

import "os"

var (
	osReadFile = os.ReadFile
	osHostname = os.Hostname
)
