package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleAuthTokenEscapesCredentialForJavaScript(t *testing.T) {
	server := &Server{options: &Options{Credential: "user:p'ass\nword"}}
	request := httptest.NewRequest("GET", "/auth_token.js", nil)
	response := httptest.NewRecorder()

	server.handleAuthToken(response, request)

	if response.Code != 200 {
		t.Fatalf("status = %d", response.Code)
	}
	body := response.Body.String()
	if strings.Contains(body, "p'ass\nword") || !strings.Contains(body, `"user:p'ass\nword"`) {
		t.Fatalf("credential was not safely encoded: %q", body)
	}
}
