package dotenv

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\nexport WEBTMUX_PORT=9090\nWEBTMUX_USERNAME='alice smith'\nWEBTMUX_PASSWORD=\"line\\nvalue\"\nEMPTY=\nPLAIN=value # comment\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, mode, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"WEBTMUX_PORT": "9090", "WEBTMUX_USERNAME": "alice smith", "WEBTMUX_PASSWORD": "line\nvalue", "EMPTY": "", "PLAIN": "value"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("values = %#v, want %#v", got, want)
	}
	if mode != 0o600 {
		t.Fatalf("mode = %o", mode)
	}
}

func TestReadRejectsInvalidLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("NOT VALID\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(path); err == nil {
		t.Fatal("invalid assignment was accepted")
	}
}
