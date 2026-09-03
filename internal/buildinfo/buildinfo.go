package buildinfo

import (
	_ "embed"
	"encoding/json"
)

//go:embed app.json
var configJSON []byte

type Info struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	URL      string `json:"url"`
	URLLabel string `json:"urlLabel"`
}

func Get() Info {
	var info Info
	if err := json.Unmarshal(configJSON, &info); err != nil {
		return Info{Name: "Таймер докладов", Version: "0.0.0"}
	}
	return info
}
