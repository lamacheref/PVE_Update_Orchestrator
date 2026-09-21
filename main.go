// Command pve-orchestrator pilote la maintenance du cluster Proxmox.
//
// Lot 0 — socle minimal : affiche la version.
// L'orchestration complète est spécifiée dans PROJET.md.
package main

import (
	"flag"
	"fmt"
	"os"
)

// version est surchargée à la compilation :
// go build -ldflags "-X main.version=$(cat VERSION)" .
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "affiche la version et quitte")
	flag.Parse()

	if *showVersion {
		fmt.Printf("pve-orchestrator %s\n", version)
		return
	}

	fmt.Printf("pve-orchestrator %s — socle Lot 0 (voir PROJET.md)\n", version)
	fmt.Println("Usage: pve-orchestrator --version")
	os.Exit(0)
}
