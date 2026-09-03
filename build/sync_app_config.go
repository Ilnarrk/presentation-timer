//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type wailsConfig struct {
	Info struct {
		ProductName    string `json:"productName"`
		ProductVersion string `json:"productVersion"`
	} `json:"info"`
}

type appConfig struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	URL      string `json:"url"`
	URLLabel string `json:"urlLabel"`
}

func main() {
	root := filepath.Join("..")
	if err := run(root); err != nil {
		fmt.Fprintf(os.Stderr, "sync app config: %v\n", err)
		os.Exit(1)
	}
}

func run(root string) error {
	wailsPath := filepath.Join(root, "wails.json")
	wailsData, err := os.ReadFile(wailsPath)
	if err != nil {
		return err
	}

	var wails wailsConfig
	if err := json.Unmarshal(wailsData, &wails); err != nil {
		return err
	}

	appPath := filepath.Join(root, "build", "app.json")
	app := appConfig{}
	if data, err := os.ReadFile(appPath); err == nil {
		if err := json.Unmarshal(data, &app); err != nil {
			return err
		}
	}

	if wails.Info.ProductName != "" {
		app.Name = wails.Info.ProductName
	}
	if wails.Info.ProductVersion != "" {
		app.Version = wails.Info.ProductVersion
	}

	out, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.WriteFile(appPath, out, 0o644); err != nil {
		return err
	}

	embeddedPath := filepath.Join(root, "internal", "buildinfo", "app.json")
	return os.WriteFile(embeddedPath, out, 0o644)
}
