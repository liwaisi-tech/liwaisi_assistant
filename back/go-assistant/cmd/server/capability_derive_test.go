package main

import (
	"reflect"
	"sort"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

func TestDeriveCapabilities_TableDriven(t *testing.T) {
	present := func(name string) persist.BinaryProbe { return persist.BinaryProbe{Name: name, Present: true} }
	presentV := func(name, ver string) persist.BinaryProbe {
		return persist.BinaryProbe{Name: name, Present: true, Version: ver}
	}

	tests := []struct {
		name        string
		binaries    []persist.BinaryProbe
		wantCapSats map[string]bool
	}{
		{
			name:     "empty_host",
			binaries: nil,
			wantCapSats: map[string]bool{
				"can-compile-c": false, "can-compile-go": false,
				"can-run-python": false, "can-run-node": false,
				"can-fetch-url": false, "can-sandbox": false,
				"can-version-control": false, "can-pack": false,
			},
		},
		{
			name: "gcc_present_enables_c",
			binaries: []persist.BinaryProbe{
				present("gcc"), presentV("go", "1.22.3"), present("python3"),
			},
			wantCapSats: map[string]bool{
				"can-compile-c": true, "can-compile-go": true, "can-run-python": true,
			},
		},
		{
			name: "go_version_floor_rejects_old",
			binaries: []persist.BinaryProbe{
				presentV("go", "1.21.0"),
			},
			wantCapSats: map[string]bool{
				"can-compile-go": false,
			},
		},
		{
			name: "clang_alone_enables_c",
			binaries: []persist.BinaryProbe{
				present("clang"),
			},
			wantCapSats: map[string]bool{
				"can-compile-c": true,
			},
		},
		{
			name: "wget_alone_enables_fetch",
			binaries: []persist.BinaryProbe{
				present("wget"),
			},
			wantCapSats: map[string]bool{
				"can-fetch-url": true,
			},
		},
		{
			name: "bwrap_enables_sandbox",
			binaries: []persist.BinaryProbe{
				present("bwrap"),
			},
			wantCapSats: map[string]bool{
				"can-sandbox": true,
			},
		},
		{
			name: "git_and_tar",
			binaries: []persist.BinaryProbe{
				present("git"), present("tar"),
			},
			wantCapSats: map[string]bool{
				"can-version-control": true, "can-pack": true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caps := DeriveCapabilities(persist.HostIdentity{}, persist.HostKernel{}, tc.binaries)
			got := map[string]bool{}
			for _, c := range caps {
				got[c.Name] = c.Satisfied
			}
			for k, want := range tc.wantCapSats {
				if got[k] != want {
					t.Errorf("%s: got %v, want %v", k, got[k], want)
				}
			}
		})
	}
}

func TestDeriveCapabilities_ReturnsAllEight(t *testing.T) {
	caps := DeriveCapabilities(persist.HostIdentity{}, persist.HostKernel{}, nil)
	names := make([]string, len(caps))
	for i, c := range caps {
		names[i] = c.Name
	}
	sort.Strings(names)
	want := []string{
		"can-compile-c", "can-compile-go", "can-fetch-url", "can-pack",
		"can-run-node", "can-run-python", "can-sandbox", "can-version-control",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestGoVersionAtLeast(t *testing.T) {
	tests := []struct {
		got, floor string
		want       bool
	}{
		{"1.22.3", "1.22", true},
		{"1.22", "1.22", true},
		{"1.21.0", "1.22", false},
		{"1.23.0", "1.22", true},
		{"2.0.0", "1.22", true},
		{"", "1.22", false},
		{"garbage", "1.22", false},
		{"1.22rc1", "1.22", true},
	}
	for _, tc := range tests {
		if got := goVersionAtLeast(tc.got, tc.floor); got != tc.want {
			t.Errorf("goVersionAtLeast(%q, %q) = %v, want %v", tc.got, tc.floor, got, tc.want)
		}
	}
}

func TestParseBinaryVersion(t *testing.T) {
	tests := []struct {
		name, banner, want string
	}{
		{"gcc", "gcc (Ubuntu 13.2.0-4ubuntu3) 13.2.0", "13.2.0"},
		{"go", "go version go1.22.3 linux/amd64", "1.22.3"},
		{"python3", "Python 3.11.6", "3.11.6"},
		{"node", "v20.11.1", "20.11.1"},
		{"ssh", "OpenSSH_9.6p1, OpenSSL 3.0.11 19 Sep 2023", "9.6p1"},
		{"jq", "jq-1.7.1", "1.7.1"},
		{"curl", "curl 8.5.0 (x86_64-pc-linux-gnu) libcurl/8.5.0", "8.5.0"},
		{"bash", "GNU bash, version 5.2.21(1)-release (x86_64-pc-linux-gnu)", "5.2.21"},
		{"unknown", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseBinaryVersion(tc.name, tc.banner)
			if got != tc.want {
				t.Errorf("ParseBinaryVersion(%q, %q) = %q, want %q", tc.name, tc.banner, got, tc.want)
			}
		})
	}
}
