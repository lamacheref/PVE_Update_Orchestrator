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
	"strconv"
	"strings"
	"time"

	"github.com/lamacheref/pve-update-orchestrator/internal/bootstrap"
	"github.com/lamacheref/pve-update-orchestrator/internal/cluster"
	"github.com/lamacheref/pve-update-orchestrator/internal/config"
	"github.com/lamacheref/pve-update-orchestrator/internal/inventory"
	"github.com/lamacheref/pve-update-orchestrator/internal/runner"
	"github.com/lamacheref/pve-update-orchestrator/internal/server"
	"github.com/lamacheref/pve-update-orchestrator/internal/sshpool"
	"gopkg.in/yaml.v3"
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
	case "cluster":
		err = cmdCluster(ctx, os.Args[2:])
	case "run":
		err = cmdRun(ctx, os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
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
  pve-orchestrator cluster discover --seed H [--nodes-file F] [--key F | --seed-key F] [--sync] [--out F]
  pve-orchestrator bootstrap-ssh --seed-key F [--nodes-file F] [--nodes A,B] [--key F] [--yes]
  pve-orchestrator bootstrap-ssh --reconcile [--seed H] [--seed-key F] [--yes]
  pve-orchestrator bootstrap-ssh --rotate --seed-key F [--yes]
  pve-orchestrator bootstrap-ssh --revoke [--seed-key F] [--yes]
  pve-orchestrator run [--nodes A,B] [--vmids 100,101] [--only os|docker] [--skip T..] [--dry-run|--apply --yes] [--no-reboot] [--resume ID]
  pve-orchestrator serve [--listen :8080] [--state-dir state]`)
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
	rotate := fs.Bool("rotate", false, "nouvelle clé : déploie puis révoque l'ancienne")
	revoke := fs.Bool("revoke", false, "purge la clé dédiée des authorized_keys")
	reconcile := fs.Bool("reconcile", false, "découvre le cluster et converge la clé partout")
	seed := fs.String("seed", "", "hôte seed pour la découverte (défaut : 1er node)")
	yes := fs.Bool("yes", false, "confirme les modifications (modifie les nodes !)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	modes := 0
	for _, m := range []bool{*rotate, *revoke, *reconcile} {
		if m {
			modes++
		}
	}
	if modes > 1 {
		return fmt.Errorf("--rotate, --revoke et --reconcile sont exclusifs")
	}

	pools := func(keyFile string, acceptNew bool) (*sshpool.Pool, error) {
		return sshpool.New(sshpool.Options{
			User: "root", KeyFile: keyFile, KnownHostsFile: *knownHosts, AcceptNewKeys: acceptNew,
		})
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

	switch {
	case *revoke:
		if !*yes {
			return fmt.Errorf("refusé sans --yes : purge /root/.authorized_keys des nodes")
		}
		nodes, err := config.LoadNodes(*nodesFile)
		if err != nil {
			return err
		}
		nodes = config.FilterNodes(nodes, csvSet(*only))
		keyFile := *newKey
		if *seedKey != "" {
			keyFile = *seedKey // la dédiée est peut-être déjà cassée : passe par la seed
		}
		pool, err := pools(keyFile, true)
		if err != nil {
			return err
		}
		defer pool.Close()
		return runRevoke(ctx, pool.Run, nodes, pub)
	case *rotate:
		if *seedKey == "" || !*yes {
			return fmt.Errorf("--rotate exige --seed-key et --yes")
		}
		seedPool, err := pools(*seedKey, true)
		if err != nil {
			return fmt.Errorf("pool seed : %w", err)
		}
		defer seedPool.Close()
		nodes, err := config.LoadNodes(*nodesFile)
		if err != nil {
			return err
		}
		return runRotate(ctx, seedPool, *newKey, *knownHosts, config.FilterNodes(nodes, csvSet(*only)))
	case *reconcile:
		if !*yes {
			return fmt.Errorf("refusé sans --yes : déploie la clé sur le cluster découvert")
		}
		return runReconcile(ctx, *nodesFile, csvSet(*only), *newKey, *seedKey, *knownHosts, *seed, pub)
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

// runRevoke purge la clé dédiée des authorized_keys (lignes du projet).
func runRevoke(ctx context.Context, run bootstrap.Runner, nodes []config.Node, pub string) error {
	if len(nodes) == 0 {
		return fmt.Errorf("aucun node après filtre")
	}
	fail := 0
	for _, n := range nodes {
		fmt.Printf("→ %s (%s)… ", n.Name, n.IP)
		nb, err := bootstrap.RevokeAuthorizedKey(ctx, run, n.IP, pub)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			fail++
			continue
		}
		fmt.Printf("✅ %d ligne(s) purgée(s)\n", nb)
	}
	fmt.Printf("— %d OK / %d en erreur\n", len(nodes)-fail, fail)
	if fail > 0 {
		return fmt.Errorf("%d node(s) en échec", fail)
	}
	return nil
}

// runRotate génère une clé fraîche, la déploie, la vérifie, puis révoque
// l'ancienne partout. En cas d'échec, l'ancienne reste en place.
func runRotate(ctx context.Context, seedPool *sshpool.Pool, keyPath, knownHosts string, nodes []config.Node) error {
	if len(nodes) == 0 {
		return fmt.Errorf("aucun node après filtre")
	}
	newPub, prevPub, cleanup, err := bootstrap.RotateKeyFiles(keyPath)
	if err != nil {
		return err
	}
	newFP, _ := bootstrap.Fingerprint(newPub)
	fmt.Printf("🔑 nouvelle clé : %s (%s)\n", keyPath, newFP)
	if prevPub == "" {
		fmt.Println("💡 première génération : rien à révoquer.")
		return nil
	}
	prevFP, _ := bootstrap.Fingerprint(prevPub)
	fmt.Printf("🗝️ ancienne clé à révoquer : %s\n", prevFP)

	newPool, err := sshpool.New(sshpool.Options{User: "root", KeyFile: keyPath, KnownHostsFile: knownHosts})
	if err != nil {
		return err
	}
	defer newPool.Close()

	fail := 0
	for _, n := range nodes {
		fmt.Printf("→ %s (%s)… ", n.Name, n.IP)
		if err := bootstrap.EnsureAuthorizedKey(ctx, seedPool.Run, n.IP, newPub); err != nil {
			fmt.Printf("❌ déploiement : %v\n", err)
			fail++
			continue
		}
		if _, err := newPool.Run(ctx, n.IP, "hostname"); err != nil {
			fmt.Printf("❌ vérif nouvelle clé : %v\n", err)
			fail++
			continue
		}
		nb, err := bootstrap.RevokeAuthorizedKey(ctx, seedPool.Run, n.IP, prevPub)
		if err != nil {
			fmt.Printf("❌ révocation ancienne : %v\n", err)
			fail++
			continue
		}
		fmt.Printf("✅ déployée + vérifiée, ancienne purgée (%d ligne(s))\n", nb)
	}
	fmt.Printf("— %d OK / %d en erreur\n", len(nodes)-fail, fail)
	if fail > 0 {
		return fmt.Errorf("%d node(s) en échec : ancienne clé conservée, .prev en place", fail)
	}
	cleanup()
	fmt.Println("🧹 .prev nettoyés : rotation terminée.")
	return nil
}

// discoverNodes découvre le cluster via le seed avec la dédiée puis la seed.
func discoverNodes(ctx context.Context, nodesFile string, newKey, seedKey, knownHosts, seed string) ([]cluster.Discovered, string, error) {
	nodes, err := config.LoadNodes(nodesFile)
	if err != nil {
		return nil, "", err
	}
	seedHost := seed
	if seedHost == "" {
		if len(nodes) == 0 {
			return nil, "", fmt.Errorf("nodes.yaml vide et --seed absent")
		}
		seedHost = nodes[0].IP
	} else {
		for _, n := range nodes {
			if n.Name == seed {
				seedHost = n.IP
			}
		}
	}
	mkPool := func(key string) (*sshpool.Pool, error) {
		return sshpool.New(sshpool.Options{User: "root", KeyFile: key, KnownHostsFile: knownHosts})
	}
	pool, err := mkPool(newKey)
	if err != nil {
		return nil, "", err
	}
	defer pool.Close()
	if _, err := pool.Run(ctx, seedHost, "hostname"); err != nil {
		if seedKey == "" {
			return nil, "", fmt.Errorf("seed %s injoignable avec la clé dédiée (et pas de --seed-key) : %w", seedHost, err)
		}
		fmt.Fprintf(os.Stderr, "⚠️ clé dédiée KO sur %s, bascule seed pour la découverte\n", seedHost)
		pool.Close()
		pool, err = sshpool.New(sshpool.Options{User: "root", KeyFile: seedKey, KnownHostsFile: knownHosts, AcceptNewKeys: true})
		if err != nil {
			return nil, "", err
		}
		defer pool.Close()
	}
	disc, err := cluster.Discover(ctx, pool.Run, seedHost)
	if err != nil {
		return nil, "", err
	}
	if errs := cluster.ResolveAll(ctx, pool.Run, seedHost, disc); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "⚠️ %v\n", e)
		}
	}
	return disc, seedHost, nil
}

// runReconcile découvre le cluster et converge la clé dédiée partout.
func runReconcile(ctx context.Context, nodesFile string, only map[string]bool, newKey, seedKey, knownHosts, seed, pub string) error {
	nodes, err := config.LoadNodes(nodesFile)
	if err != nil {
		return err
	}
	disc, seedHost, err := discoverNodes(ctx, nodesFile, newKey, seedKey, knownHosts, seed)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "🔍 cluster vu depuis %s : %d node(s)\n", seedHost, len(disc))
	plan := cluster.Reconcile(nodes, disc)
	for _, d := range plan.New {
		fmt.Fprintf(os.Stderr, "➕ nouveau dans le cluster : %s (%s)\n", d.Name, d.IP)
	}
	for _, g := range plan.Gone {
		fmt.Fprintf(os.Stderr, "➖ dans le fichier mais plus dans le cluster : %s\n", g.Name)
	}

	type target struct{ name, ip string }
	var targets []target
	seen := map[string]bool{}
	for _, p := range plan.Matched {
		ip := p.File.IP
		if p.Live.IP != "" && p.Live.IP != ip {
			fmt.Fprintf(os.Stderr, "🔁 %s : IP fichier %s ≠ cluster %s → cluster gagne\n", p.File.Name, ip, p.Live.IP)
			ip = p.Live.IP
		}
		targets = append(targets, target{p.File.Name, ip})
		seen[p.File.Name] = true
	}
	for _, d := range plan.New {
		if d.IP == "" {
			fmt.Fprintf(os.Stderr, "❌ %s sans IP : ignoré (ajoutez-le à %s)\n", d.Name, nodesFile)
			continue
		}
		targets = append(targets, target{d.Name, d.IP})
	}

	var withKey []target
	for _, t := range targets {
		if len(only) > 0 && !only[t.name] {
			continue
		}
		withKey = append(withKey, t)
	}
	if len(withKey) == 0 {
		return fmt.Errorf("aucune cible après filtre")
	}

	mkPool := func(key string, acceptNew bool) (*sshpool.Pool, error) {
		return sshpool.New(sshpool.Options{User: "root", KeyFile: key, KnownHostsFile: knownHosts, AcceptNewKeys: acceptNew})
	}
	mainPool, err := mkPool(newKey, false)
	if err != nil {
		return err
	}
	defer mainPool.Close()
	var seedPool *sshpool.Pool
	if seedKey != "" {
		seedPool, err = mkPool(seedKey, true)
		if err != nil {
			return err
		}
		defer seedPool.Close()
	}

	fail := 0
	for _, t := range withKey {
		fmt.Printf("→ %s (%s)… ", t.name, t.ip)
		if err := bootstrap.EnsureAuthorizedKey(ctx, mainPool.Run, t.ip, pub); err != nil {
			if seedPool == nil {
				fmt.Printf("❌ %v (pas de --seed-key de secours)\n", err)
				fail++
				continue
			}
			if err := bootstrap.EnsureAuthorizedKey(ctx, seedPool.Run, t.ip, pub); err != nil {
				fmt.Printf("❌ seed aussi : %v\n", err)
				fail++
				continue
			}
		}
		if _, err := mainPool.Run(ctx, t.ip, "hostname"); err != nil {
			// la pool cache peut-être une vieille connexion : drop + retry via re-dial interne
			fmt.Printf("❌ vérif : %v\n", err)
			fail++
			continue
		}
		fmt.Println("✅ convergé")
	}
	fmt.Printf("— %d OK / %d en erreur\n", len(withKey)-fail, fail)
	if fail > 0 {
		return fmt.Errorf("%d node(s) en échec", fail)
	}
	return nil
}

// cmdServe lance le dashboard lecture seule.
func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := fs.String("listen", ":8080", "adresse d'écoute")
	stateDir := fs.String("state-dir", "state", "répertoire des runs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return server.Listen(*listen, *stateDir)
}

// cmdRun orchestre un run hebdo (dry-run par défaut, live = --apply --yes).
func cmdRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	nodesFile := fs.String("nodes-file", "configs/nodes.yaml", "fichier des nodes")
	cfgFile := fs.String("config", "", "config.yaml (défauts SSH/PBS/Discord)")
	onlyNodes := fs.String("nodes", "", "filtre CSV")
	onlyVMIDs := fs.String("vmids", "", "filtre CSV (ex. 100,101)")
	only := fs.String("only", "", "os | docker (défaut : tout)")
	skip := fs.String("skip", "", "cibles à sauter CSV (ex. node/Zeus,lxc/100)")
	dryRun := fs.Bool("dry-run", true, "plan sans toucher (défaut)")
	apply := fs.Bool("apply", false, "EXÉCUTE vraiment (exige --yes)")
	yes := fs.Bool("yes", false, "confirme le live")
	autoReboot := fs.Bool("auto-reboot", true, "reboot si nouveau kernel")
	noReboot := fs.Bool("no-reboot", false, "désactive les reboots")
	canaryFirst := fs.Bool("canary-first", true, "canary d'abord")
	workers := fs.Int("workers", 4, "parallélisme guests")
	pbs := fs.String("pbs-storage", "pbs", "datastore PBS (sans --config)")
	keep := fs.Int("keep-kernels", 2, "kernels conservés (sans --config)")
	backupTimeout := fs.Duration("backup-timeout", 0, "timeout backup PBS, 0 = quasi-illimité")
	backupWait := fs.Duration("backup-wait", 30*time.Minute, "attente max backup tiers, 0 = report immédiat")
	skipBackup := fs.Bool("skip-backup", false, "MAJ SANS snapshot PBS (assumé, tracé dans le rapport)")
	pruneAll := fs.Bool("prune-all", false, "docker image prune -a")
	stateDir := fs.String("state-dir", "state", "état runs + dashboard")
	resume := fs.String("resume", "", "run-id à reprendre")
	runID := fs.String("run-id", "", "run-id explicite")
	user := fs.String("user", "root", "SSH (sans --config)")
	key := fs.String("key", "~/.ssh/pve-orchestrator_ed25519", "clé (sans --config)")
	knownHosts := fs.String("known-hosts", "~/.ssh/known_hosts", "known_hosts")
	if err := fs.Parse(args); err != nil {
		return err
	}

	live := *apply
	if live && !*yes {
		return fmt.Errorf("live refusé sans --yes (garde-fou)")
	}
	if !live && !*dryRun {
		return fmt.Errorf("précisez --dry-run (plan) ou --apply --yes (live)")
	}
	if *only != "" && *only != "os" && *only != "docker" {
		return fmt.Errorf("--only doit valoir os ou docker")
	}

	nodes, err := config.LoadNodes(*nodesFile)
	if err != nil {
		return err
	}
	var cfg *config.Config
	if *cfgFile != "" {
		c, err := config.LoadConfig(*cfgFile)
		if err != nil {
			return err
		}
		cfg = &c
	}
	pool, err := sshpool.New(sshOptionsFromFlags(nil, *user, *key, *knownHosts, cfg))
	if err != nil {
		return err
	}
	defer pool.Close()

	vmids := map[int]bool{}
	for _, s := range strings.Split(*onlyVMIDs, ",") {
		if s = strings.TrimSpace(s); s != "" {
			id, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("--vmids invalide %q", s)
			}
			vmids[id] = true
		}
	}
	pbsStorage, keepKernels := *pbs, *keep
	webhook := os.Getenv("DISCORD_WEBHOOK_UPDATEUR")
	if cfg != nil {
		pbsStorage, keepKernels = cfg.PBS.Storage, cfg.Update.KeepKernels
		if w, err := cfg.DiscordWebhook(); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️ discord désactivé : %v\n", err)
		} else {
			webhook = w
		}
	}
	o := runner.Options{
		RunID: stRunID(*runID, *resume), NodeFilter: csvSet(*onlyNodes), VMIDs: vmids,
		Only: *only, Skip: csvSet(*skip), DryRun: !live,
		AutoReboot: *autoReboot && !*noReboot, CanaryFirst: *canaryFirst,
		Workers: *workers, PBSStorage: pbsStorage, KeepKernels: keepKernels,
		BackupTimeout: *backupTimeout, BackupWait: *backupWait, SkipBackup: *skipBackup,
		PruneAll: *pruneAll, StateDir: *stateDir, Resume: *resume, WebhookURL: webhook,
	}
	st, err := runner.Run(ctx, pool.Run, nodes, o)
	if err != nil {
		return err
	}
	ok, fail := 0, 0
	for _, tr := range st.Targets {
		if tr.OK && !tr.Hold {
			ok++
		} else if !tr.OK {
			fail++
		}
	}
	fmt.Fprintf(os.Stderr, "🏁 run %s : %d OK / %d FAIL (%d cibles)\n", st.RunID, ok, fail, len(st.Targets))
	if fail > 0 {
		return fmt.Errorf("%d cible(s) en échec", fail)
	}
	return nil
}

func stRunID(explicit, resume string) string {
	if explicit != "" {
		return explicit
	}
	return resume // "" = nouveau run (runner génère)
}
func cmdCluster(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("cluster discover", flag.ContinueOnError)
	if len(args) < 1 || args[0] != "discover" {
		return fmt.Errorf("usage : pve-orchestrator cluster discover [options]")
	}
	nodesFile := fs.String("nodes-file", "configs/nodes.yaml", "fichier des nodes")
	seed := fs.String("seed", "", "nom/IP seed (défaut : 1er node du fichier)")
	newKey := fs.String("key", "~/.ssh/pve-orchestrator_ed25519", "clé dédiée")
	seedKey := fs.String("seed-key", "", "clé seed de secours")
	knownHosts := fs.String("known-hosts", "~/.ssh/known_hosts", "known_hosts")
	sync := fs.Bool("sync", false, "ajoute les nouveaux nodes à nodes.yaml (.bak)")
	out := fs.String("out", "", "fichier JSON de sortie (défaut : stdout)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	disc, seedHost, err := discoverNodes(ctx, *nodesFile, *newKey, *seedKey, *knownHosts, *seed)
	if err != nil {
		return err
	}
	nodes, err := config.LoadNodes(*nodesFile)
	if err != nil {
		return err
	}
	plan := cluster.Reconcile(nodes, disc)

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if *out != "" {
		if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
			return err
		}
	} else {
		fmt.Println(string(data))
	}
	fmt.Fprintf(os.Stderr, "🔍 seed %s : %d matchés, %d nouveaux, %d disparus\n",
		seedHost, len(plan.Matched), len(plan.New), len(plan.Gone))

	if *sync {
		added := 0
		known := map[string]bool{}
		for _, n := range nodes {
			known[n.Name] = true
		}
		for _, d := range plan.New {
			if d.IP == "" || known[d.Name] {
				continue
			}
			nodes = append(nodes, config.Node{Name: d.Name, IP: d.IP, Role: "standard"})
			added++
		}
		if added == 0 {
			fmt.Fprintln(os.Stderr, "💾 rien à synchroniser")
			return nil
		}
		raw, err := os.ReadFile(*nodesFile)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*nodesFile+".bak", raw, 0o644); err != nil {
			return err
		}
		out, err := yaml.Marshal(config.NodesFile{Nodes: nodes})
		if err != nil {
			return err
		}
		header := "# Généré/étendu par `cluster discover --sync` — backup : " + *nodesFile + ".bak\n"
		if err := os.WriteFile(*nodesFile, append([]byte(header), out...), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "💾 %d node(s) ajoutés à %s\n", added, *nodesFile)
	}
	return nil
}
