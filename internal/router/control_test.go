package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func controlRequest(handler http.Handler, method, path string, body []byte, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://localhost"+path, bytes.NewReader(body))
	request.Host = "localhost"
	request.RemoteAddr = "127.0.0.1:43210"
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestControlAPIRegistrationLifecycle(t *testing.T) {
	dir := t.TempDir()
	token, err := EnsureControlToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "control.token"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("control token permissions: %v, %v", info, err)
	}
	handler := Handler(dir, true)

	response := controlRequest(handler, http.MethodGet, "/_loca/v1/health", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("health status %d: %s", response.Code, response.Body)
	}
	response = controlRequest(handler, http.MethodPost, "/_loca/v1/dev-services", []byte(`{"hostname":"App.Project.localhost","port":43127}`), "wrong")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized registration status %d", response.Code)
	}
	response = controlRequest(handler, http.MethodPost, "/_loca/v1/dev-services", []byte(`{"hostname":"App.Project.localhost","port":43127}`), token)
	if response.Code != http.StatusCreated {
		t.Fatalf("registration status %d: %s", response.Code, response.Body)
	}
	var registered registerDevServiceResponse
	if err = json.Unmarshal(response.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}
	if registered.Hostname != "app.project.localhost" || registered.Port != 43127 || registered.Owner == "" {
		t.Fatalf("unexpected registration: %+v", registered)
	}
	services, err := ReadDevServices(dir)
	if err != nil || services["app.project"].Port != 43127 {
		t.Fatalf("registration not persisted: %+v, %v", services, err)
	}

	body, _ := json.Marshal(unregisterDevServiceRequest{Hostname: registered.Hostname, Owner: "another-process"})
	response = controlRequest(handler, http.MethodDelete, "/_loca/v1/dev-services", body, token)
	if response.Code != http.StatusOK {
		t.Fatalf("mismatched-owner cleanup status %d", response.Code)
	}
	services, _ = ReadDevServices(dir)
	if _, ok := services["app.project"]; !ok {
		t.Fatal("cleanup from another process removed registration")
	}

	body, _ = json.Marshal(unregisterDevServiceRequest{Hostname: registered.Hostname, Owner: registered.Owner})
	response = controlRequest(handler, http.MethodDelete, "/_loca/v1/dev-services", body, token)
	if response.Code != http.StatusOK {
		t.Fatalf("cleanup status %d", response.Code)
	}
	services, _ = ReadDevServices(dir)
	if _, ok := services["app.project"]; ok {
		t.Fatal("registration was not removed")
	}
}

func TestControlAPIRejectsBrowserOriginsAndRemoteRequests(t *testing.T) {
	dir := t.TempDir()
	token, err := EnsureControlToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	handler := ControlHandler(dir)
	request := httptest.NewRequest(http.MethodPost, "http://localhost/_loca/v1/dev-services", bytes.NewBufferString(`{"hostname":"app.localhost","port":43127}`))
	request.Host = "localhost"
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("browser Origin status %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "http://localhost/_loca/v1/health", nil)
	request.Host = "localhost"
	request.RemoteAddr = "192.0.2.10:1234"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("remote health request status %d", response.Code)
	}
}
