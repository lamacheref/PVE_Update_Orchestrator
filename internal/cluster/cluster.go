// Package cluster découvre les membres réels du cluster depuis Proxmox
// (pvecm nodes) et les réconcilie avec nodes.yaml.
//
// Le programme se débrouille seul : une seed joignable suffit pour
// découvrir les autres nodes, résoudre leurs IPs et converger l'inventaire.
package cluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/lamacheref/pve-update-orchestrator/internal/config"
)

// Runner exécute une commande sur un hôte (ex. méthode *sshpool.Pool.Run).
type Runner func(ctx context.Context, host, cmd string) (string, error)

// Discovered est un node vu par le cluster.
type Discovered struct {
	NodeID int    `json:"nodeid"`
	Name   string `json:"name"`
	Votes  int    `json:"votes"`
	IP     string `json:"ip,omitempty"`
}

// ParseNodes lit `pvecm nodes` (gère le suffixe " (local)").
func ParseNodes(out string) ([]Discovered, error) {
	var nodes []Discovered
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		id, err1 := strconv.Atoi(f[0])
		votes, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue // en-tête, séparateurs…
		}
		name := strings.TrimSuffix(strings.Join(f[2:], " "), " (local)")
		name = strings.TrimSpace(name)
		if name == "" || name == "Name" {
			continue
		}
		nodes = append(nodes, Discovered{NodeID: id, Name: name, Votes: votes})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("pvecm nodes : aucun node parsé")
	}
	return nodes, nil
}

// Discover interroge pvecm nodes sur l'hôte seed.
func Discover(ctx context.Context, run Runner, seedHost string) ([]Discovered, error) {
	out, err := run(ctx, seedHost, "pvecm nodes")
	if err != nil {
		return nil, fmt.Errorf("découverte via %s : %w", seedHost, err)
	}
	return ParseNodes(out)
}

// ResolveIP résout le nom via getent sur le seed (IPs du cluster, pas du DNS public).
func ResolveIP(ctx context.Context, run Runner, seedHost, name string) (string, error) {
	out, err := run(ctx, seedHost, "getent hosts "+name+" | awk '{print $1}' | head -1")
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(out)
	if ip == "" {
		return "", fmt.Errorf("IP de %s introuvable (getent vide)", name)
	}
	return ip, nil
}

// ResolveAll remplit les IPs ; les échecs sont renvoyés sans bloquer les autres.
func ResolveAll(ctx context.Context, run Runner, seedHost string, nodes []Discovered) []error {
	var errs []error
	for i := range nodes {
		ip, err := ResolveIP(ctx, run, seedHost, nodes[i].Name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		nodes[i].IP = ip
	}
	return errs
}

// Pair associe un node du fichier à sa découverte.
type Pair struct {
	File config.Node
	Live Discovered
}

// Plan est le résultat de la réconciliation.
type Plan struct {
	Matched []Pair
	New     []Discovered  // dans le cluster, absents du fichier
	Gone    []config.Node // dans le fichier, absents du cluster
}

// Reconcile compare nodes.yaml au cluster réel.
// Le match se fait par nom insensible à la casse (Proxmox renvoie souvent
// des minuscules : "janus" vs "Janus") ; le nom du fichier est conservé.
func Reconcile(file []config.Node, live []Discovered) Plan {
	var p Plan
	byName := map[string]Discovered{}
	for _, d := range live {
		byName[strings.ToLower(d.Name)] = d
	}
	seen := map[string]bool{}
	for _, n := range file {
		key := strings.ToLower(n.Name)
		if d, ok := byName[key]; ok {
			p.Matched = append(p.Matched, Pair{File: n, Live: d})
			seen[key] = true
		} else {
			p.Gone = append(p.Gone, n)
		}
	}
	for _, d := range live {
		if !seen[strings.ToLower(d.Name)] {
			p.New = append(p.New, d)
		}
	}
	return p
}
