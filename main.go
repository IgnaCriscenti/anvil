package main

import (
	"fmt"
	"os"

	"github.com/Watts-Argentina/anvil/ui"
)

// version se inyecta al compilar con -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("anvil", version)
		return
	}
	ui.Start()
}
