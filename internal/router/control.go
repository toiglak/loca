package router

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const controlPrefix = "/_loca/v1/"

type controlResponse struct {
	Error string `json:"error,omitempty"`
}

func isLocalControlRequest(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if !strings.EqualFold(host, "localhost") {
		return false
	}
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(remote)
	return ip != nil && ip.IsLoopback()
}

func writeControlJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func controlError(w http.ResponseWriter, status int, message string) {
	writeControlJSON(w, status, controlResponse{Error: message})
}

func controlToken(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "control.token"))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func authorizedControlRequest(dir string, r *http.Request) bool {
	if r.Header.Get("Origin") != "" {
		return false
	}
	want, err := controlToken(dir)
	if err != nil {
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

type registerDevServiceRequest struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
}

type registerDevServiceResponse struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	Owner    string `json:"owner"`
}

func randomOwner() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func ControlHandler(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalControlRequest(r) {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == controlPrefix+"health" && r.Method == http.MethodGet {
			writeControlJSON(w, http.StatusOK, map[string]any{"service": "loca", "apiVersion": 1})
			return
		}
		if !authorizedControlRequest(dir, r) {
			controlError(w, http.StatusUnauthorized, "invalid or missing Loca control token")
			return
		}
		switch {
		case r.URL.Path == controlPrefix+"dev-services" && r.Method == http.MethodPost:
			registerDevService(dir, w, r)
		case r.URL.Path == controlPrefix+"dev-services" && r.Method == http.MethodDelete:
			unregisterDevService(dir, w, r)
		default:
			controlError(w, http.StatusNotFound, "unknown Loca control endpoint")
		}
	})
}

func registerDevService(dir string, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request registerDevServiceRequest
	if err := decoder.Decode(&request); err != nil {
		controlError(w, http.StatusBadRequest, "invalid registration body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		controlError(w, http.StatusBadRequest, "registration body must contain one JSON object")
		return
	}
	name, err := HostName(request.Hostname)
	if err != nil || request.Port < 1 || request.Port > 65535 || request.Port == 80 || request.Port == 443 {
		controlError(w, http.StatusBadRequest, "use a valid .localhost hostname and application port other than 80/443")
		return
	}
	owner, err := randomOwner()
	if err != nil {
		controlError(w, http.StatusInternalServerError, "could not create registration owner")
		return
	}
	if err = UpdateDevServices(dir, func(services map[string]DevService) error {
		services[name] = DevService{Port: request.Port, Owner: owner}
		return nil
	}); err != nil {
		controlError(w, http.StatusInternalServerError, "could not save development service")
		return
	}
	writeControlJSON(w, http.StatusCreated, registerDevServiceResponse{Hostname: name + ".localhost", Port: request.Port, Owner: owner})
}

type unregisterDevServiceRequest struct {
	Hostname string `json:"hostname"`
	Owner    string `json:"owner"`
}

func unregisterDevService(dir string, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request unregisterDevServiceRequest
	if err := decoder.Decode(&request); err != nil {
		controlError(w, http.StatusBadRequest, "invalid unregistration body")
		return
	}
	name, err := HostName(request.Hostname)
	if err != nil || request.Owner == "" {
		controlError(w, http.StatusBadRequest, "use a valid .localhost hostname and registration owner")
		return
	}
	removed := false
	if err = UpdateDevServices(dir, func(services map[string]DevService) error {
		if current, ok := services[name]; ok && current.Owner == request.Owner {
			delete(services, name)
			removed = true
		}
		return nil
	}); err != nil {
		controlError(w, http.StatusInternalServerError, "could not remove development service")
		return
	}
	writeControlJSON(w, http.StatusOK, map[string]bool{"removed": removed})
}
