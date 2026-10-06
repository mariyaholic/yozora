package main

import (
	"os"

	"uika-resonance/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
