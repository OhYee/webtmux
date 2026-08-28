package keys

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsesConfiguredKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	data := []byte(`{
		"row": [{"label": "ESC only", "seq": "\u001b"}],
		"groups": [{"label": "Custom", "keys": [{"label": "Break", "seq": "\u0003"}]}]
	}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	panel, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(panel.Row) != 1 || panel.Row[0].Label != "ESC only" {
		t.Fatalf("row = %#v, want configured key only", panel.Row)
	}
	if len(panel.Groups) != 1 || panel.Groups[0].Label != "Custom" {
		t.Fatalf("groups = %#v, want configured group", panel.Groups)
	}
}

func TestLoadAllowsEmptyFirstTab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(path, []byte(`{"row":[],"groups":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	panel, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(panel.Row) != 0 || len(panel.Groups) != 0 {
		t.Fatalf("panel = %#v, want an entirely user-controlled empty panel", panel)
	}
}

func TestSaveAtomicallyReplacesConfiguredPanel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(path, []byte(`{"row":[],"groups":[]}`), 0o640); err != nil {
		t.Fatal(err)
	}

	panel := &Panel{
		Row: []Key{{Label: "ESC", Seq: "\x1b"}},
		Groups: []Group{{
			Label: "Custom",
			Keys:  []Key{{Label: "Break", Seq: "\x03"}},
		}},
	}
	if err := Save(path, panel); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Row) != 1 || got.Row[0].Seq != "\x1b" {
		t.Fatalf("saved panel = %#v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o640 {
		t.Fatalf("mode = %o, want 640", gotMode)
	}
}

func TestSaveRejectsInvalidKeyWithoutReplacingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	original := []byte(`{"row":[],"groups":[]}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	err := Save(path, &Panel{Row: []Key{{Label: "missing sequence"}}})
	if err == nil {
		t.Fatal("Save accepted a key without a sequence")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("invalid save changed file to %q", got)
	}
}
