package webassets

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var frontend embed.FS

func FS() fs.FS {
	assets, err := fs.Sub(frontend, "dist")
	if err != nil {
		panic(err)
	}
	if _, err := fs.Stat(assets, "generated/index.html"); err == nil {
		generated, err := fs.Sub(assets, "generated")
		if err != nil {
			panic(err)
		}
		return generated
	}
	return assets
}
