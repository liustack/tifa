package ssr

import (
	"embed"
	"encoding/json"
	"io/fs"
)

//go:embed assets
var embedded embed.FS

type Build struct {
	BuildID string `json:"buildId"`
	Script  string `json:"script"`
	Style   string `json:"style"`
}

type Bundle struct {
	Build  Build
	Routes []byte
	Public fs.FS
	code   string
}

func LoadBundle() (*Bundle, error) {
	b := &Bundle{}
	build, err := embedded.ReadFile("assets/build.json")
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(build, &b.Build); err != nil {
		return nil, err
	}
	if b.Routes, err = embedded.ReadFile("assets/routes.json"); err != nil {
		return nil, err
	}
	code, err := embedded.ReadFile("assets/server.js")
	if err != nil {
		return nil, err
	}
	b.code = string(code)
	b.Public, err = fs.Sub(embedded, "assets/public")
	return b, err
}
