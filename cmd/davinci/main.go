// Command davinci is a local, AI-native image editor. This binary is the
// server (which owns every document and applies every edit), the store and
// the AI-facing CLI; the browser page is a CanvasKit view of the document.
package main

import (
	"os"

	"davinci/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
