// Package inventory collecte l'état réel du cluster via le pool SSH.
//
// Lecture seule : pvecm status, uname, pveversion, pct/qm list.
// Chaque node alimente NodeInfo ; un échec node n'arrête pas les autres
// (erreurs dans CheckErrors, l'appelant décide).
package inventory

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/lamacheref/pve-update-orchestrator/internal/config"
)

// Runner exécute une commande sur un node (ex. méthode *sshpool.Pool.Run).
type Runner func(ctx context.Context, host, cmd string) (string, error)

// Guest est un LXC ou une VM.
type Guest struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Kind   string `json:"kind"` // lxc | qemu
	Node   string `json:"node"`
}

// NodeInfo est l'état collecté d'un node.
type NodeInfo struct {
	Name       string   `json:"name"`
	IP         string   `json:"ip"`
	Kernel     string   `json:"kernel"`
	PVEVersion string   `json:"pve_version"`
	Quorate    bool     `json:"quorate"`
	NodeCount  int      `json:"node_count"`
	Guests     []Guest  `json:"guests"`
	Errors     []string `json:"errors,omitempty"`
}

var (
	reQuorum = regexp.MustCompile(`(?m)^\s*Quorat(?:e|um):\s*(Yes|No)`)
	reNodes  = regexp.MustCompile(`(?m)^\s*Nodes:\s*(\d+)`)
)

// ParseQuorum lit pvecm status (gère "Quorate:" et "Quorum:").
func ParseQuorum(out string) bool {
	m := reQuorum.FindStringSubmatch(out)
	return len(m) == 2 && m[1] == "Yes"
}

// ParseNodeCount lit "Nodes: N" de pvecm status (0 si absent).
func ParseNodeCount(out string) int {
	m := reNodes.FindStringSubmatch(out)
	if len(m) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// ParseKernel rend la 1re ligne (uname -r).
func ParseKernel(out string) string {
	return firstLine(out)
}

// ParsePVEVersion rend la 1re ligne (pve-manager/…).
func ParsePVEVersion(out string) string {
	return firstLine(out)
}

func firstLine(out string) string {
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	return strings.TrimSpace(out)
}

// ParsePCT lit `pct list` (VMID Status [Lock] Name).
func ParsePCT(node string, out string) ([]Guest, error) {
	var guests []Guest
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] == "VMID" {
			continue
		}
		id, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		guests = append(guests, Guest{VMID: id, Name: f[len(f)-1], Status: strings.ToLower(f[1]), Kind: "lxc", Node: node})
	}
	return guests, nil
}

// ParseQM lit `qm list` (VMID NAME STATUS …).
func ParseQM(node string, out string) ([]Guest, error) {
	var guests []Guest
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] == "VMID" {
			continue
		}
		id, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		guests = append(guests, Guest{VMID: id, Name: f[1], Status: strings.ToLower(f[2]), Kind: "qemu", Node: node})
	}
	return guests, nil
}

func collectOne(ctx context.Context, run Runner, n config.Node) NodeInfo {
	info := NodeInfo{Name: n.Name, IP: n.IP}
	runStep := func(cmd string, apply func(string)) {
		if ctx.Err() != nil {
			info.Errors = append(info.Errors, "annulé")
			return
		}
		out, err := run(ctx, n.IP, cmd)
		if err != nil {
			info.Errors = append(info.Errors, fmt.Sprintf("%s : %v", cmd, err))
			return
		}
		apply(out)
	}
	runStep("pvecm status", func(out string) {
		info.Quorate = ParseQuorum(out)
		info.NodeCount = ParseNodeCount(out)
	})
	runStep("uname -r", func(out string) { info.Kernel = ParseKernel(out) })
	runStep("pveversion | head -1", func(out string) { info.PVEVersion = ParsePVEVersion(out) })
	runStep("pct list", func(out string) {
		g, _ := ParsePCT(n.Name, out)
		info.Guests = append(info.Guests, g...)
	})
	runStep("qm list", func(out string) {
		g, _ := ParseQM(n.Name, out)
		info.Guests = append(info.Guests, g...)
	})
	return info
}

// Sync collecte tous les nodes en parallèle bornée, ordre du slice préservé.
func Sync(ctx context.Context, run Runner, nodes []config.Node, workers int) []NodeInfo {
	if workers < 1 {
		workers = 1
	}
	out := make([]NodeInfo, len(nodes))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, n := range nodes {
		wg.Add(1)
		go func(i int, n config.Node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = collectOne(ctx, run, n)
		}(i, n)
	}
	wg.Wait()
	return out
}

// HasErrors rend vrai si au moins un node a des erreurs.
func HasErrors(infos []NodeInfo) bool {
	for _, in := range infos {
		if len(in.Errors) > 0 {
			return true
		}
	}
	return false
}
