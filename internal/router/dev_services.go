package router

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type DevService struct {
	Port  int    `json:"port"`
	Owner string `json:"owner"`
}

func ReadDevServices(dir string) (map[string]DevService, error) {
	data, err := os.ReadFile(filepath.Join(dir, "dev-services.json"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]DevService{}, nil
	}
	if err != nil {
		return nil, err
	}
	services := map[string]DevService{}
	if err := json.Unmarshal(data, &services); err != nil {
		return nil, fmt.Errorf("invalid development service file: %w", err)
	}
	for name, service := range services {
		clean, err := Name(name)
		if err != nil || clean != name || service.Port < 1 || service.Port > 65535 || service.Port == 80 || service.Port == 443 || service.Owner == "" {
			return nil, fmt.Errorf("invalid development service %q", name)
		}
	}
	return services, nil
}

func UpdateDevServices(dir string, update func(map[string]DevService) error) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "mappings.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	services, err := ReadDevServices(dir)
	if err != nil {
		return err
	}
	if err = update(services); err != nil {
		return err
	}
	data, err := json.MarshalIndent(services, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(dir, "dev-services.json"), append(data, '\n'), 0600)
}

func EnsureControlToken(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "control.token")
	token, err := os.ReadFile(path)
	if err == nil {
		if len(token) != 64 {
			return "", fmt.Errorf("invalid Loca control token")
		}
		if err = os.Chmod(path, 0600); err != nil {
			return "", err
		}
		return string(token), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	value := make([]byte, 32)
	if _, err = rand.Read(value); err != nil {
		return "", err
	}
	encoded := []byte(hex.EncodeToString(value))
	if err = AtomicWrite(path, encoded, 0600); err != nil {
		return "", err
	}
	return string(encoded), nil
}
