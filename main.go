// Command pve-orchestrator pilote la maintenance du cluster Proxmox.
// Spec : PROJET.md. Lot 0 : inventory sync, bootstrap-ssh.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/lamacheref/pve-update-orchestrator/internal/bootstrap"
	"github.com/lamacheref/pve-update-orchestrator/internal/config"
	"github.com/lamacheref/pve-update-orchestrator/internal/inventory"
	"github.com/lamacheref/pve-update-orchestrator/internal/sshpool"
)

// version est surchargée à la compilation :
// go build -ldflags "-X main.version=$(cat VERSION)" .
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var err error
	switch os.Args[1] {
	case "--version", "version":
		fmt.Printf("pve-orchestrator %s\n", version)
	case "inventory":
		err = cmdInventory(ctx, os.Args[2:])
	case "bootstrap-ssh":
		err = cmdBootstrap(ctx, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "commande inconnue : %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`pve-orchestrator ` + version + ` — voir PROJET.md
Usage:
  pve-orchestrator --version
  pve-orchestrator inventory sync [--nodes-file F] [--config F] [--nodes A,B] [--workers N] [--out F]
  pve-orchestrator bootstrap-ssh --seed-key F [--nodes-file F] [--nodes A,B] [--key F] [--generate-only] [--yes]`)
}

func csvSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out[p] = true
		}
	}
	return out
}

func sshOptionsFromFlags(fs *flag.FlagSet, user, key, knownHosts string, c *config.Config) sshpool.Options {
	if c != nil {
		return sshpool.Options{
			User: c.SSH.User, Port: c.SSH.Port,
			KeyFile: c.SSH.KeyFile, KnownHostsFile: c.SSH.KnownHosts,
			DialTimeout:    time.Duration(c.SSH.DialTimeout),
			CommandTimeout: time.Duration(c.SSH.CommandTimeout),
			Keepalive:      time.Duration(c.SSH.Keepalive),
		}
	}
	return sshpool.Options{User: user, KeyFile: key, KnownHostsFile: knownHosts}
}

func cmdInventory(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("inventory sync", flag.ContinueOnError)
	if len(args) < 1 || args[0] != "sync" {
		return fmt.Errorf("usage : pve-orchestrator inventory sync [options]")
	}
	nodesFile := fs.String("nodes-file", "configs/nodes.yaml", "fichier des nodes")
	cfgFile := fs.String("config", "", "config.yaml (optionnel : défauts SSH sinon)")
	only := fs.String("nodes", "", "filtre CSV (ex. Janus,Zeus)")
	workers := fs.Int("workers", 4, "parallélisme")
	out := fs.String("out", "", "fichier JSON de sortie (défaut : stdout)")
	user := fs.String("user", "root", "utilisateur SSH (sans --config)")
	key := fs.String("key", "~/.ssh/pve-orchestrator_ed25519", "clé privée (sans --config)")
	knownHosts := fs.String("known-hosts", "~/.ssh/known_hosts", "known_hosts (sans --config)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	nodes, err := config.LoadNodes(*nodesFile)
	if err != nil {
		return err
	}
	nodes = config.FilterNodes(nodes, csvSet(*only))
	if len(nodes) == 0 {
		return fmt.Errorf("aucun node après filtre")
	}

	var cfg *config.Config
	if *cfgFile != "" {
		c, err := config.LoadConfig(*cfgFile)
		if err != nil {
			return err
		}
		cfg = &c
	}
	pool, err := sshpool.New(sshOptionsFromFlags(fs, *user, *key, *knownHosts, cfg))
	if err != nil {
		return err
	}
	defer pool.Close()

	fmt.Fprintf(os.Stderr, "🗺️ inventaire de %d node(s)…\n", len(nodes))
	infos := inventory.Sync(ctx, pool.Run, nodes, *workers)

	data, err := json.MarshalIndent(infos, "", "  ")
	if err != nil {
		return err
	}
	if *out != "" {
		if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "💾 %s\n", *out)
	} else {
		fmt.Println(string(data))
	}

	ok, fail := 0, 0
	for _, in := range infos {
		if len(in.Errors) > 0 {
			fail++
			fmt.Fprintf(os.Stderr, "❌ %s (%s) : %s\n", in.Name, in.IP, strings.Join(in.Errors, " | "))
		} else {
			ok++
			fmt.Fprintf(os.Stderr, "✅ %s : quorum=%v guests=%d kernel=%s\n", in.Name, in.Quorate, len(in.Guests), in.Kernel)
		}
	}
	fmt.Fprintf(os.Stderr, "— %d OK / %d en erreur\n", ok, fail)
	if fail > 0 {
		return fmt.Errorf("%d node(s) en erreur", fail)
	}
	return nil
}

func cmdBootstrap(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("bootstrap-ssh", flag.ContinueOnError)
	nodesFile := fs.String("nodes-file", "configs/nodes.yaml", "fichier des nodes")
	only := fs.String("nodes", "", "filtre CSV (ex. Janus pour le canary)")
	seedKey := fs.String("seed-key", "", "clé seed initiale (REQUISE sauf --generate-only)")
	newKey := fs.String("key", "~/.ssh/pve-orchestrator_ed25519", "clé dédiée à déployer")
	knownHosts := fs.String("known-hosts", "~/.ssh/known_hosts", "known_hosts")
	genOnly := fs.Bool("generate-only", false, "génère la clé sans rien déployer")
	yes := fs.Bool("yes", false, "confirme le déploiement (modifie les nodes !)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pub, created, err := bootstrap.GenerateKey(*newKey)
	if err != nil {
		return err
	}
	fp, _ := bootstrap.Fingerprint(pub)
	if created {
		fmt.Printf("🔑 clé générée : %s (%s)\n", *newKey, fp)
	} else {
		fmt.Printf("🔑 clé existante : %s (%s)\n", *newKey, fp)
	}
	if *genOnly {
		return nil
	}
	if *seedKey == "" {
		return fmt.Errorf("--seed-key requise (ou --generate-only)")
	}
	if !*yes {
		return fmt.Errorf("refusé sans --yes : le déploiement ajoute la clé à /root/.authorized_keys des nodes")
	}

	nodes, err := config.LoadNodes(*nodesFile)
	if err != nil {
		return err
	}
	nodes = config.FilterNodes(nodes, csvSet(*only))
	if len(nodes) == 0 {
		return fmt.Errorf("aucun node après filtre")
	}

	seedPool, err := sshpool.New(sshpool.Options{
		User: "root", KeyFile: *seedKey, KnownHostsFile: *knownHosts, AcceptNewKeys: true,
	})
	if err != nil {
		return fmt.Errorf("pool seed : %w", err)
	}
	defer seedPool.Close()

	newPool, err := sshpool.New(sshpool.Options{
		User: "root", KeyFile: *newKey, KnownHostsFile: *knownHosts,
	})
	if err != nil {
		return fmt.Errorf("pool dédiée : %w", err)
	}
	defer newPool.Close()

	fail := 0
	for _, n := range nodes {
		fmt.Printf("→ %s (%s)… ", n.Name, n.IP)
		if err := bootstrap.EnsureAuthorizedKey(ctx, seedPool.Run, n.IP, pub); err != nil {
			fmt.Printf("❌ déploiement : %v\n", err)
			fail++
			continue
		}
		out, err := newPool.Run(ctx, n.IP, "hostname && pvecm status | grep -iE 'quorat'")
		if err != nil {
			fmt.Printf("❌ vérif clé dédiée : %v\n", err)
			fail++
			continue
		}
		fmt.Printf("✅ %s\n", strings.TrimSpace(strings.ReplaceAll(out, "\n", " ")))
	}
	fmt.Printf("— %d OK / %d en erreur\n", len(nodes)-fail, fail)
	if fail > 0 {
		return fmt.Errorf("%d node(s) en échec (seed révoquée ? réseau ?)", fail)
	}
	fmt.Println("💡 la seed ne sert plus : révoquez-la / archivez-la hors ligne.")
	return nil
}
