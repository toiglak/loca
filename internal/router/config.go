package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

type Config map[string]int

var validLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func Name(raw string) (string, error) {
	name := strings.TrimSuffix(strings.ToLower(raw), ".localhost")
	if len(name)+len(".localhost") > 253 {
		return "", fmt.Errorf("invalid service name %q: hostname is too long", raw)
	}
	for _, label := range strings.Split(name, ".") {
		if !validLabel.MatchString(label) {
			return "", fmt.Errorf("invalid service name %q: use dot-separated labels of 1–63 letters, digits or hyphens, starting and ending with a letter or digit", raw)
		}
	}
	return name, nil
}
func Dir() string {
	if dir := os.Getenv("LOCA_DATA_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "loca")
}
func Read(dir string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(dir, "services.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var config Config
	if err = json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("invalid mapping file: %w", err)
	}
	if config == nil {
		config = Config{}
	}
	for name, port := range config {
		if clean, e := Name(name); e != nil || clean != name || port < 1 || port > 65535 || port == 80 || port == 443 {
			return nil, fmt.Errorf("invalid mapping %q: %d", name, port)
		}
	}
	return config, nil
}
func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".loca-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func Update(dir string, fn func(Config) error) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "mappings.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	c, err := Read(dir)
	if err != nil {
		return err
	}
	if err = fn(c); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(dir, "services.json"), append(data, '\n'), 0600)
}
