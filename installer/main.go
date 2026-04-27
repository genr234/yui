package main

import (
	"embed"
	"io/fs"
	"log"
)

//go:embed assets/*
var assets embed.FS

func main() {
	if _, err := fs.ReadFile(assets, "assets/chrome.bat"); err != nil {
		log.Fatal(err)
	}
}

