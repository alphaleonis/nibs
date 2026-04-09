package main

import (
	"io/fs"

	"github.com/alphaleonis/nibs/cmd"
)

func main() {
	// Strip the "web/dist" prefix so the FS root is the dist directory.
	distFS, err := fs.Sub(webDistFS, "web/dist")
	if err != nil {
		panic(err)
	}
	cmd.WebDistFS = distFS
	cmd.Execute()
}
