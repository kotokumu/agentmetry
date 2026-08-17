package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGenerateFingerprint(t *testing.T) {
	singleRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(singleRoot, "AGENTS.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	multipleRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(multipleRoot, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(multipleRoot, "AGENTS.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(multipleRoot, ".codex", "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unicodeRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(unicodeRoot, "指示.md"), []byte("こんにちは\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideRoot := t.TempDir()
	outsideFileRoot := t.TempDir()
	outsideFile := filepath.Join(outsideFileRoot, "AGENTS.md")
	if err := os.WriteFile(outsideFile, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(symlinkRoot, "real.md"), []byte("real\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.md", filepath.Join(symlinkRoot, "link.md")); err != nil {
		t.Fatal(err)
	}
	nonUTF8Root := t.TempDir()
	nonUTF8Name := string([]byte{0xff})
	directoryRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(directoryRoot, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	type args struct {
		root  string
		scope string
		label string
		files []string
	}
	tests := []struct {
		name    string
		args    args
		want    Identity
		wantErr bool
	}{
		{name: "normative single file vector", args: args{root: singleRoot, scope: "project-7f2a", files: []string{"AGENTS.md"}}, want: Identity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}},
		{name: "normative multiple file vector is order independent", args: args{root: multipleRoot, scope: "project-7f2a", label: "AGENTS v2", files: []string{"AGENTS.md", ".codex/config.toml"}}, want: Identity{Scope: "project-7f2a", Fingerprint: "sha256:dfbc1de58f3b905c7b0c0fd79361699336b5f9da617b1db8f35c76673f95b29d", Label: "AGENTS v2"}},
		{name: "normative unicode vector", args: args{root: unicodeRoot, scope: "project-7f2a", files: []string{"指示.md"}}, want: Identity{Scope: "project-7f2a", Fingerprint: "sha256:2fbba0adc4e6411315c9105e6b86d4e843f499291a32e4ebfea49abf773b4bc8"}},
		{name: "requires files", args: args{root: t.TempDir(), scope: "project-7f2a"}, wantErr: true},
		{name: "rejects invalid scope", args: args{root: singleRoot, scope: "project space", files: []string{"AGENTS.md"}}, wantErr: true},
		{name: "rejects invalid label", args: args{root: singleRoot, scope: "project-7f2a", label: "before,after", files: []string{"AGENTS.md"}}, wantErr: true},
		{name: "rejects outside root", args: args{root: outsideRoot, scope: "project-7f2a", files: []string{outsideFile}}, wantErr: true},
		{name: "rejects duplicate normalized path", args: args{root: singleRoot, scope: "project-7f2a", files: []string{"AGENTS.md", "./AGENTS.md"}}, wantErr: true},
		{name: "rejects symlink", args: args{root: symlinkRoot, scope: "project-7f2a", files: []string{"link.md"}}, wantErr: true},
		{name: "rejects non utf8 path", args: args{root: nonUTF8Root, scope: "project-7f2a", files: []string{nonUTF8Name}}, wantErr: true},
		{name: "rejects missing file", args: args{root: t.TempDir(), scope: "project-7f2a", files: []string{"missing.md"}}, wantErr: true},
		{name: "rejects directory", args: args{root: directoryRoot, scope: "project-7f2a", files: []string{"config"}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateFingerprint(tt.args.root, tt.args.scope, tt.args.label, tt.args.files)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GenerateFingerprint() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !cmp.Equal(tt.want, got) {
				t.Errorf("GenerateFingerprint() = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got))
			}
		})
	}
}
