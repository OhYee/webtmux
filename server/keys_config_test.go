package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"webtmux/pkg/keys"
)

func TestKeysConfigRequiresExplicitFileForWrites(t *testing.T) {
	server := &Server{options: &Options{}}

	get := httptest.NewRecorder()
	server.handleKeysConfig(get, httptest.NewRequest(http.MethodGet, "/keys-config.json", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", get.Code, get.Body.String())
	}
	var response struct {
		Writable bool `json:"writable"`
		Content  string
	}
	if err := json.Unmarshal(get.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Writable {
		t.Fatal("default configuration must be read-only")
	}
	if response.Content == "" {
		t.Fatal("default configuration should remain inspectable")
	}

	put := httptest.NewRecorder()
	server.handleKeysConfig(put, httptest.NewRequest(http.MethodPut, "/keys-config.json", bytes.NewBufferString(`{"row":[],"groups":[]}`)))
	if put.Code != http.StatusConflict {
		t.Fatalf("PUT status = %d, want %d", put.Code, http.StatusConflict)
	}
}

func TestKeysConfigSavesConfiguredFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(path, []byte(`{"row":[],"groups":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{options: &Options{KeysFile: path}}
	body := `{"row":[{"label":"ESC","seq":"\u001b"}],"groups":[]}`

	put := httptest.NewRecorder()
	server.handleKeysConfig(put, httptest.NewRequest(http.MethodPut, "/keys-config.json", bytes.NewBufferString(body)))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", put.Code, put.Body.String())
	}

	panel, err := keys.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(panel.Row) != 1 || panel.Row[0].Seq != "\x1b" {
		t.Fatalf("saved panel = %#v", panel)
	}
}

func TestKeysConfigRejectsInvalidPanel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	original := []byte(`{"row":[],"groups":[]}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{options: &Options{KeysFile: path}}

	put := httptest.NewRecorder()
	server.handleKeysConfig(put, httptest.NewRequest(http.MethodPut, "/keys-config.json", bytes.NewBufferString(
		`{"row":[{"label":"broken","seq":""}],"groups":[]}`,
	)))
	if put.Code != http.StatusBadRequest {
		t.Fatalf("PUT status = %d, want %d", put.Code, http.StatusBadRequest)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("invalid PUT changed file to %q", got)
	}
}
