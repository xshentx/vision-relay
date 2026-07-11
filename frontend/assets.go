package frontend

import (
	"embed"
	"io/fs"
)

//go:embed public
var embeddedFS embed.FS

var FS = mustSub(embeddedFS, "public")

func mustSub(source fs.FS, directory string) fs.FS {
	sub, err := fs.Sub(source, directory)
	if err != nil {
		panic(err)
	}
	return sub
}
