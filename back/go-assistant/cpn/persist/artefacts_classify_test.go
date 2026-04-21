package persist

import (
	"io/fs"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		path string
		mode fs.FileMode
		want ArtefactClassification
	}{
		{"source_go", "/home/u/.local/brae/src/main.go", 0o644, ClassSource},
		{"source_py", "/home/u/.local/brae/scripts/run.py", 0o644, ClassSource},
		{"source_md_is_source", "/home/u/.local/brae/docs/README.md", 0o644, ClassSource},
		{"binary_no_ext", "/home/u/.local/brae/bin/brae", 0o755, ClassBinary},
		{"binary_sh_exec_still_source", "/home/u/.local/brae/tools/installer.sh", 0o755, ClassSource},
		{"config_yaml", "/home/u/.local/brae/cfg/config.yaml", 0o644, ClassConfig},
		{"config_toml", "/home/u/.local/brae/cargo/Cargo.toml", 0o644, ClassConfig},
		{"config_env_name", "/home/u/.local/brae/app/.env", 0o600, ClassConfig},
		{"config_dockerfile", "/home/u/.local/brae/Dockerfile", 0o644, ClassConfig},
		{"log_ext", "/home/u/.local/brae/app/brae.log", 0o644, ClassLog},
		{"log_dir_segment", "/home/u/.local/brae/logs/output.txt", 0o644, ClassLog},
		{"man_ext_1", "/home/u/.local/brae/man/brae.1", 0o644, ClassMan},
		{"man_path", "/home/u/.local/brae/man/brae.5", 0o644, ClassMan},
		{"other_fallback", "/home/u/.local/brae/data/blob.bin", 0o644, ClassOther},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.path, tc.mode)
			if got != tc.want {
				t.Errorf("Classify(%q, %o) = %q; want %q", tc.path, tc.mode, got, tc.want)
			}
		})
	}
}
