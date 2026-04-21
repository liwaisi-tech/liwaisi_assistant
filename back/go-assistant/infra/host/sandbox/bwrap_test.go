package sandbox

import (
	"errors"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

type fakeCap struct{ have map[string]bool }

func (f fakeCap) Available(runtime string) bool { return f.have[runtime] }

func newReq() cpn.ExecRequest {
	return cpn.ExecRequest{Command: "echo", Args: []string{"hi"}}
}

func TestWrap_NoneProfilePassthrough(t *testing.T) {
	w := New(fakeCap{have: map[string]bool{"bwrap": true}})
	out, err := w.Wrap(newReq(), cpn.SandboxNone)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Command != "echo" {
		t.Errorf("expected passthrough, got command=%q", out.Command)
	}
}

func TestWrap_Bwrap_ReadOnly(t *testing.T) {
	w := New(fakeCap{have: map[string]bool{"bwrap": true}})
	out, err := w.Wrap(newReq(), cpn.SandboxReadonly)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Command != "bwrap" {
		t.Fatalf("expected bwrap wrap, got %q", out.Command)
	}
	joined := strings.Join(out.Args, " ")
	for _, want := range []string{"--ro-bind / /", "--tmpfs /tmp", "--unshare-all", "--share-net", "-- echo hi"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in args, got %q", want, joined)
		}
	}
}

func TestWrap_Bwrap_NetworkOff(t *testing.T) {
	w := New(fakeCap{have: map[string]bool{"bwrap": true}})
	out, _ := w.Wrap(newReq(), cpn.SandboxNetworkOff)
	joined := strings.Join(out.Args, " ")
	if !strings.Contains(joined, "--unshare-net") {
		t.Errorf("expected --unshare-net, got %q", joined)
	}
}

func TestWrap_Bwrap_FSJail(t *testing.T) {
	w := New(fakeCap{have: map[string]bool{"bwrap": true}})
	w.JailRoot = "/jail"
	out, _ := w.Wrap(newReq(), cpn.SandboxFSJail)
	joined := strings.Join(out.Args, " ")
	for _, want := range []string{"--bind /jail /work", "--chdir /work"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q, got %q", want, joined)
		}
	}
}

func TestWrap_FallbackFirejail(t *testing.T) {
	w := New(fakeCap{have: map[string]bool{"firejail": true}}) // no bwrap
	out, err := w.Wrap(newReq(), cpn.SandboxReadonly)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Command != "firejail" {
		t.Errorf("expected firejail, got %q", out.Command)
	}
	if !strings.Contains(strings.Join(out.Args, " "), "--read-only=/") {
		t.Errorf("expected --read-only=/, got %q", out.Args)
	}
}

func TestWrap_NeitherRuntimeErrors(t *testing.T) {
	w := New(fakeCap{have: map[string]bool{}})
	_, err := w.Wrap(newReq(), cpn.SandboxReadonly)
	if !errors.Is(err, ErrSandboxUnavailable) {
		t.Fatalf("expected ErrSandboxUnavailable, got %v", err)
	}
}
