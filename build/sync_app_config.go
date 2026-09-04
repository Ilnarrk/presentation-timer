//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type appConfig struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	URL      string `json:"url"`
	URLLabel string `json:"urlLabel"`
}

func main() {
	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sync app config: %v\n", err)
		os.Exit(1)
	}
	if err := run(root); err != nil {
		fmt.Fprintf(os.Stderr, "sync app config: %v\n", err)
		os.Exit(1)
	}
}

// findProjectRoot locates the repo root whether the hook runs from the project
// root (wails build/dev) or from build/ (manual go run).
func findProjectRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for _, candidate := range []string{wd, filepath.Join(wd, "..")} {
		appPath := filepath.Join(candidate, "build", "app.json")
		if _, err := os.Stat(appPath); err == nil {
			return filepath.Clean(candidate), nil
		}
	}
	return "", fmt.Errorf("build/app.json not found from %s", wd)
}

func run(root string) error {
	appPath := filepath.Join(root, "build", "app.json")
	appData, err := os.ReadFile(appPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", appPath, err)
	}

	var app appConfig
	if err := json.Unmarshal(appData, &app); err != nil {
		return fmt.Errorf("parse %s: %w", appPath, err)
	}
	if version := strings.TrimSpace(os.Getenv("APP_VERSION")); version != "" {
		app.Version = strings.TrimPrefix(version, "v")
	}

	out, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	embeddedPath := filepath.Join(root, "internal", "buildinfo", "app.json")
	if err := os.WriteFile(embeddedPath, out, 0o644); err != nil {
		return err
	}

	return syncWailsMetadata(root, app)
}

func syncWailsMetadata(root string, app appConfig) error {
	wailsPath := filepath.Join(root, "wails.json")
	wailsData, err := os.ReadFile(wailsPath)
	if err != nil {
		return err
	}

	var wails map[string]interface{}
	if err := json.Unmarshal(wailsData, &wails); err != nil {
		return err
	}

	info, _ := wails["info"].(map[string]interface{})
	if info == nil {
		info = map[string]interface{}{}
		wails["info"] = info
	}
	if app.Name != "" {
		info["productName"] = app.Name
	}
	if app.Version != "" {
		info["productVersion"] = app.Version
	}

	out, err := json.MarshalIndent(wails, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(wailsPath, out, 0o644)
}
