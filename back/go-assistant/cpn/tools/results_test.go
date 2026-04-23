package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// AC-004 mirror: the WriteFileResult JSON shape MUST NOT contain a
// `content` (or alias) field. We marshal a populated value and assert
// against the wire shape directly so the regression catches accidental
// re-additions even via embedded structs.
func TestWriteFileResult_NoContentField(t *testing.T) {
	r := WriteFileResult{
		Path: "main.go", Bytes: 1247, Lines: 58,
		SHA256: "abc", Created: true,
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(b)
	for _, banned := range []string{`"content"`, `"body"`, `"bytes_written"`} {
		if strings.Contains(wire, banned) {
			t.Errorf("WriteFileResult JSON unexpectedly contains %s: %s", banned, wire)
		}
	}
	for _, must := range []string{`"path"`, `"bytes"`, `"lines"`, `"sha256"`, `"created"`} {
		if !strings.Contains(wire, must) {
			t.Errorf("WriteFileResult JSON missing required field %s: %s", must, wire)
		}
	}
}

func TestEditFileResult_DiffNotFullBody(t *testing.T) {
	r := EditFileResult{
		Path: "main.go", BytesAfter: 1305, LinesAfter: 61,
		Diff: "@@ -1,3 +1,3 @@\n-old\n+new\n",
	}
	b, _ := json.Marshal(r)
	wire := string(b)
	for _, banned := range []string{`"content"`, `"body"`, `"after"`} {
		if strings.Contains(wire, banned) {
			t.Errorf("EditFileResult JSON unexpectedly contains %s: %s", banned, wire)
		}
	}
}

func TestWriteFileResult_SizeLabel(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{0, "0B"},
		{843, "843B"},
		{1024, "1.0KB"},
		{1247, "1.2KB"},
		{5 * 1024 * 1024, "5.0MB"},
	}
	for _, tc := range cases {
		got := WriteFileResult{Bytes: tc.bytes}.SizeLabel()
		if got != tc.want {
			t.Errorf("SizeLabel(%d)=%q want %q", tc.bytes, got, tc.want)
		}
	}
}

func TestMaxEditDiffLines_Sane(t *testing.T) {
	if MaxEditDiffLines < 50 || MaxEditDiffLines > 1000 {
		t.Errorf("MaxEditDiffLines=%d outside sane range", MaxEditDiffLines)
	}
}
