// Package update applique les mises à jour sur nodes et guests.
//
// Cycle par cible : capture avant → topgrade → autoremove → capture après
// → reboot si kernel → kernel-clean. DryRun = commandes journalisées,
// rien d'exécuté.
package update

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lamacheref/pve-update-orchestrator/internal/health"
	"github.com/lamacheref/pve-update-orchestrator/internal/inventory"
)

// Runner exécute une commande sur un hôte.
type Runner func(ctx context.Context, host, cmd string) (string, error)

// Options du run.
type Options struct {
	DryRun      bool
	AutoReboot  bool
	KeepKernels int
	PBSStorage  string
	PruneAll    bool // docker image prune -a (défaut : system prune seul)
	RebootWait  time.Duration
}

// Result résume la mise à jour d'une cible.
type Result struct {
	Target       string   `json:"target"`
	Kind         string   `json:"kind"` // node | lxc | qemu
	OK           bool     `json:"ok"`
	DryRun       bool     `json:"dry_run"`
	PkgsBefore   int      `json:"pkgs_before"`
	PkgsAfter    int      `json:"pkgs_after"`
	KernelBefore string   `json:"kernel_before"`
	KernelAfter  string   `json:"kernel_after"`
	Rebooted     bool     `json:"rebooted"`
	BackupID     string   `json:"backup_id,omitempty"`
	Duration     string   `json:"duration"`
	Log          []string `json:"log,omitempty"`
	Error        string   `json:"error,omitempty"`
}

func (r *Result) fail(format string, a ...any) {
	r.OK = false
	r.Error = fmt.Sprintf(format, a...)
	r.Log = append(r.Log, "ERROR: "+r.Error)
}

func (r *Result) note(format string, a ...any) {
	r.Log = append(r.Log, fmt.Sprintf(format, a...))
}

// Dry convertit un Runner en journaliseur (aucune exécution).
func Dry() Runner {
	return func(_ context.Context, host, cmd string) (string, error) {
		return "[dry-run] " + host + " $ " + cmd, nil
	}
}

// UpgradableCount compte `apt list --upgradable` (hors ligne Listing…).
func UpgradableCount(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Listing") {
			continue
		}
		n++
	}
	return n
}

const (
	aptBefore = `uname -r; echo ---APT---; apt list --upgradable 2>/dev/null | grep -v -c '^Listing'`
	aptAfter  = aptBefore
)

// parseBeforeAfter lit "kernel\n---APT---\nN".
func parseBeforeAfter(out string) (kernel string, pkgs int) {
	parts := strings.SplitN(out, "---APT---", 2)
	kernel = strings.TrimSpace(strings.Split(parts[0], "\n")[0])
	if len(parts) == 2 {
		pkgs, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
	}
	return kernel, pkgs
}

var reKernelPkg = regexp.MustCompile(`^ii\s+(pve-kernel-\S+)\s`)
var reKernelVer = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)-(\d+)`)

// KernelPackages extrait les paquets pve-kernel installés de `dpkg -l`.
func KernelPackages(dpkgOut string) []string {
	var pkgs []string
	for _, line := range strings.Split(dpkgOut, "\n") {
		if m := reKernelPkg.FindStringSubmatch(line); len(m) == 2 {
			if !strings.Contains(m[1], "headers") && !strings.Contains(m[1], "modules") {
				pkgs = append(pkgs, m[1])
			}
		}
	}
	return pkgs
}

func kernelRank(pkg string) [4]int {
	var r [4]int
	if m := reKernelVer.FindStringSubmatch(pkg); len(m) == 5 {
		for i := 0; i < 4; i++ {
			r[i], _ = strconv.Atoi(m[i+1])
		}
	}
	return r
}

// SelectKernelsToPurge choisit les vieux kernels à purger.
// Jamais le kernel booté (running) ; garde les `keep` plus récents (running inclus).
func SelectKernelsToPurge(installed []string, running string, keep int) []string {
	if keep < 1 {
		keep = 1
	}
	// Filtre les méta-paquets (pve-kernel-6.8 sans suffixe de version).
	var versioned []string
	for _, p := range installed {
		if reKernelVer.MatchString(p) {
			versioned = append(versioned, p)
		}
	}
	sort.Slice(versioned, func(i, j int) bool {
		ri, rj := kernelRank(versioned[i]), kernelRank(versioned[j])
		for k := 0; k < 4; k++ {
			if ri[k] != rj[k] {
				return ri[k] > rj[k]
			}
		}
		return versioned[i] > versioned[j]
	})
	kept := map[string]bool{}
	for _, p := range versioned {
		if strings.Contains(p, running) {
			kept[p] = true // le booté survit toujours
		}
	}
	for _, p := range versioned {
		if len(kept) >= keep {
			break
		}
		kept[p] = true
	}
	var purge []string
	for _, p := range versioned {
		if !kept[p] {
			purge = append(purge, p)
		}
	}
	return purge
}

// waitSSHUp attend le retour SSH après reboot.
func waitSSHUp(ctx context.Context, run Runner, host string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		if _, err := run(ctx, host, "true"); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout %s : %s toujours injoignable", wait, host)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}

// UpdateNode met à jour un node Proxmox.
func UpdateNode(ctx context.Context, run Runner, host, name string, o Options) Result {
	t0 := time.Now()
	r := Result{Target: "node/" + name, Kind: "node", OK: true, DryRun: o.DryRun}
	exec := run
	if o.DryRun {
		exec = Dry()
	}

	before, err := exec(ctx, host, aptBefore)
	if err != nil {
		r.fail("capture avant : %v", err)
		return r
	}
	if o.DryRun {
		r.KernelBefore, r.PkgsBefore = "dry-kernel", -1
		r.note("dry-run : %s", firstLine(before))
	} else {
		r.KernelBefore, r.PkgsBefore = parseBeforeAfter(before)
	}
	r.note("avant : kernel=%s upgradable=%d", r.KernelBefore, r.PkgsBefore)

	if _, err := exec(ctx, host, "topgrade -y --only system"); err != nil {
		r.fail("topgrade : %v", err)
		return r
	}
	if _, err := exec(ctx, host, "DEBIAN_FRONTEND=noninteractive apt-get autoremove -y && apt-get clean"); err != nil {
		r.fail("autoremove : %v", err)
		return r
	}

	after, err := exec(ctx, host, aptAfter)
	if err != nil {
		r.fail("capture après : %v", err)
		return r
	}
	if o.DryRun {
		r.KernelAfter, r.PkgsAfter = "dry-kernel", -1
	} else {
		r.KernelAfter, r.PkgsAfter = parseBeforeAfter(after)
	}
	r.note("après : kernel=%s upgradable=%d", r.KernelAfter, r.PkgsAfter)

	changed := r.KernelBefore != r.KernelAfter
	if changed && !o.DryRun && o.AutoReboot {
		r.note("nouveau kernel → reboot")
		if _, err := run(ctx, host, "reboot"); err != nil {
			r.note("reboot émis (erreur attendue, connexion coupée) : %v", err)
		}
		wait := o.RebootWait
		if wait == 0 {
			wait = 15 * time.Minute
		}
		t1 := time.Now()
		// Laisse la machine partir avant de poller.
		select {
		case <-ctx.Done():
			r.fail("annulé pendant le reboot")
			return r
		case <-time.After(45 * time.Second):
		}
		if err := waitSSHUp(ctx, run, host, wait); err != nil {
			r.fail("retour reboot : %v", err)
			return r
		}
		r.Rebooted = true
		r.note("reboot OK en %s", time.Since(t1).Round(time.Second))
	} else if changed {
		r.note("nouveau kernel, reboot skippé (dry-run ou auto-reboot off)")
	}

	if !o.DryRun {
		if err := kernelCleanNode(ctx, run, host, r.KernelAfter, o.KeepKernels, &r); err != nil {
			r.fail("kernel-clean : %v", err)
			return r
		}
	}
	r.Duration = time.Since(t0).Round(time.Second).String()
	return r
}

// kernelCleanNode purge les vieux pve-kernel (jamais le booté) + refresh boot-tool.
func kernelCleanNode(ctx context.Context, run Runner, host, running string, keep int, r *Result) error {
	out, err := run(ctx, host, "dpkg -l 'pve-kernel-*' 2>/dev/null | grep '^ii'")
	if err != nil {
		return err
	}
	purge := SelectKernelsToPurge(KernelPackages(out), running, keep)
	if len(purge) == 0 {
		r.note("kernel-clean : rien à purger (keep=%d)", keep)
		return nil
	}
	r.note("kernel-clean : purge %s", strings.Join(purge, " "))
	if _, err := run(ctx, host, "DEBIAN_FRONTEND=noninteractive apt-get purge -y "+strings.Join(purge, " ")); err != nil {
		return err
	}
	if _, err := run(ctx, host, "proxmox-boot-tool refresh"); err != nil {
		r.note("boot-tool refresh : %v (non bloquant)", err)
	}
	return nil
}

// guestShell construit la commande hôte pour un guest.
func guestShell(g inventory.Guest, inner string) string {
	q := "'" + strings.ReplaceAll(inner, "'", `'\''`) + "'"
	if g.Kind == "lxc" {
		return fmt.Sprintf("pct exec %d -- bash -c %s", g.VMID, q)
	}
	return fmt.Sprintf("qm guest exec %d --timeout 1800 -- bash -c %s", g.VMID, q)
}

// qemuRun exécute via l'agent (JSON) avec suivi async si timeout dépassé.
func qemuRun(ctx context.Context, run Runner, nodeIP string, vmid int, inner string) (string, error) {
	out, err := run(ctx, nodeIP, fmt.Sprintf("qm guest exec %d --timeout 1800 -- bash -c %s", vmid, shellQ(inner)))
	if err != nil {
		return out, err
	}
	code, data := health.ParseGuestExec(out)
	if code >= 0 {
		if code != 0 {
			return data, fmt.Errorf("exitcode=%d : %s", code, data)
		}
		return data, nil
	}
	// Pas d'exitcode : run async, on suit via le pid.
	pid := extractPID(out)
	if pid == "" {
		return out, fmt.Errorf("sortie agent inattendue : %s", truncate(out, 200))
	}
	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(60 * time.Second):
		}
		st, err := run(ctx, nodeIP, fmt.Sprintf("qm guest exec-status %d %s", vmid, pid))
		if err != nil {
			continue
		}
		if strings.Contains(st, `"exited" : 1`) || strings.Contains(st, `"exited":1`) {
			code, data := health.ParseGuestExec(st)
			if code != 0 {
				return data, fmt.Errorf("exitcode=%d : %s", code, data)
			}
			return data, nil
		}
	}
	return "", fmt.Errorf("agent : pid %s toujours en cours après 30 min", pid)
}

var rePID = regexp.MustCompile(`"pid"\s*:\s*(\d+)`)

func extractPID(out string) string {
	if m := rePID.FindStringSubmatch(out); len(m) == 2 {
		return m[1]
	}
	return ""
}

func shellQ(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// BackupGuest snapshotte un guest vers PBS (fail-closed).
func BackupGuest(ctx context.Context, run Runner, nodeIP string, g inventory.Guest, storage, runID string) (string, error) {
	out, err := run(ctx, nodeIP, fmt.Sprintf("vzdump %d --mode snapshot --storage %s --compress zstd --notes-template 'pre-update %s'",
		g.VMID, storage, runID))
	if err != nil {
		return "", fmt.Errorf("vzdump : %w (sortie : %s)", err, truncate(out, 300))
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Finished Backup") {
			return backupVolume(out), nil
		}
	}
	return "", fmt.Errorf("vzdump sans marqueur Finished Backup (sortie : %s)", truncate(out, 300))
}

var reBackupVol = regexp.MustCompile(`Backup Volume:\s*(\S+)`)

func backupVolume(out string) string {
	if m := reBackupVol.FindStringSubmatch(out); len(m) == 2 {
		return m[1]
	}
	return "vzdump-ok"
}

// UpdateGuest met à jour un LXC/VM via le node hôte.
func UpdateGuest(ctx context.Context, run Runner, nodeIP string, g inventory.Guest, o Options, runID string) Result {
	t0 := time.Now()
	r := Result{Target: fmt.Sprintf("%s/%d@%s", g.Kind, g.VMID, g.Name), Kind: g.Kind, OK: true, DryRun: o.DryRun}
	exec := run
	if o.DryRun {
		exec = Dry()
	}

	if !o.DryRun {
		id, err := BackupGuest(ctx, run, nodeIP, g, o.PBSStorage, runID)
		if err != nil {
			r.fail("backup PBS (fail-closed) : %v", err)
			return r
		}
		r.BackupID = id
		r.note("backup PBS : %s", id)
	} else {
		r.note("dry-run : backup skippé")
	}

	before := ""
	var err error
	if g.Kind == "lxc" {
		before, err = exec(ctx, nodeIP, guestShell(g, aptBefore))
	} else {
		before, err = qemuExecBefore(ctx, exec, run, nodeIP, g, o.DryRun)
	}
	if err != nil {
		r.fail("capture avant : %v", err)
		return r
	}
	if o.DryRun {
		r.KernelBefore, r.PkgsBefore = "dry-kernel", -1
	} else {
		r.KernelBefore, r.PkgsBefore = parseBeforeAfter(before)
	}
	r.note("avant : kernel=%s upgradable=%d", r.KernelBefore, r.PkgsBefore)

	updateCmd := "topgrade -y --only system && DEBIAN_FRONTEND=noninteractive apt-get autoremove -y && apt-get clean"
	if g.Kind == "lxc" {
		if _, err := exec(ctx, nodeIP, guestShell(g, updateCmd)); err != nil {
			r.fail("update : %v", err)
			return r
		}
	} else if _, err := qemuExecUpdate(ctx, exec, run, nodeIP, g, updateCmd, o.DryRun); err != nil {
		r.fail("update : %v", err)
		return r
	}

	after := ""
	if g.Kind == "lxc" {
		after, err = exec(ctx, nodeIP, guestShell(g, aptAfter))
	} else {
		after, err = qemuExecBefore(ctx, exec, run, nodeIP, g, o.DryRun)
	}
	if err != nil {
		r.fail("capture après : %v", err)
		return r
	}
	if o.DryRun {
		r.KernelAfter, r.PkgsAfter = "dry-kernel", -1
	} else {
		r.KernelAfter, r.PkgsAfter = parseBeforeAfter(after)
	}
	r.note("après : kernel=%s upgradable=%d", r.KernelAfter, r.PkgsAfter)

	changed := r.KernelBefore != r.KernelAfter
	switch {
	case g.Kind == "lxc":
		r.note("LXC : kernel partagé avec l'hôte, pas de reboot guest")
	case changed && !o.DryRun && o.AutoReboot:
		r.note("nouveau kernel VM → reboot")
		if _, err := run(ctx, nodeIP, fmt.Sprintf("qm reboot %d", g.VMID)); err != nil {
			r.fail("reboot VM : %v", err)
			return r
		}
		if err := waitAgentUp(ctx, run, nodeIP, g.VMID, 10*time.Minute); err != nil {
			r.fail("retour VM : %v", err)
			return r
		}
		r.Rebooted = true
	case changed:
		r.note("nouveau kernel, reboot skippé (dry-run ou auto-reboot off)")
	}
	r.Duration = time.Since(t0).Round(time.Second).String()
	return r
}

func qemuExecBefore(ctx context.Context, exec, run Runner, nodeIP string, g inventory.Guest, dry bool) (string, error) {
	if dry {
		return exec(ctx, nodeIP, guestShell(g, aptBefore))
	}
	return qemuRun(ctx, run, nodeIP, g.VMID, aptBefore)
}

func qemuExecUpdate(ctx context.Context, exec, run Runner, nodeIP string, g inventory.Guest, cmd string, dry bool) (string, error) {
	if dry {
		return exec(ctx, nodeIP, guestShell(g, cmd))
	}
	return qemuRun(ctx, run, nodeIP, g.VMID, cmd)
}

// waitAgentUp attend le retour de l'agent après reboot VM.
func waitAgentUp(ctx context.Context, run Runner, nodeIP string, vmid int, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	time.Sleep(30 * time.Second) // laisse la VM partir
	for {
		if _, err := run(ctx, nodeIP, fmt.Sprintf("qm agent %d ping", vmid)); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("agent VM %d toujours KO après %s", vmid, wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}
}

// HasDocker détecte docker dans le guest (dry-run → true pour traverser le plan).
func HasDocker(ctx context.Context, run Runner, nodeIP string, g inventory.Guest, dry bool) bool {
	if dry {
		return true
	}
	var out string
	var err error
	if g.Kind == "lxc" {
		out, err = run(ctx, nodeIP, guestShell(g, "command -v docker || true"))
	} else {
		out, err = qemuRun(ctx, run, nodeIP, g.VMID, "command -v docker || true")
	}
	return err == nil && strings.TrimSpace(out) != ""
}

// UpdateDocker met à jour la couche Docker d'un guest (après UpdateGuest).
func UpdateDocker(ctx context.Context, run Runner, nodeIP string, g inventory.Guest, o Options) Result {
	t0 := time.Now()
	r := Result{Target: fmt.Sprintf("docker/%d@%s", g.VMID, g.Name), Kind: "docker", OK: true, DryRun: o.DryRun}
	exec := run
	if o.DryRun {
		exec = Dry()
	}
	has, err := exec(ctx, nodeIP, guestShell(g, "command -v docker || true"))
	if err != nil || (!o.DryRun && strings.TrimSpace(has) == "") {
		if err != nil {
			r.fail("détection docker : %v", err)
			return r
		}
		r.note("pas de docker, rien à faire")
		r.Duration = time.Since(t0).Round(time.Second).String()
		return r
	}
	projects, err := exec(ctx, nodeIP, guestShell(g, "docker compose ls --format '{{.Name}} {{.ConfigFiles}}' 2>/dev/null || true"))
	if err != nil {
		r.fail("compose ls : %v", err)
		return r
	}
	n := 0
	if !o.DryRun {
		for _, line := range strings.Split(projects, "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			cfg := strings.Split(f[1], ",")[0]
			if _, err := exec(ctx, nodeIP, guestShell(g, fmt.Sprintf("docker compose -f %s pull && docker compose -f %s up -d", cfg, cfg))); err != nil {
				r.note("compose %s : %v", f[0], err)
				continue
			}
			n++
		}
		r.note("%d projet(s) compose pull+up", n)
	} else {
		r.note("dry-run : compose pull+up skippés")
	}
	prune := "docker system prune -f"
	if o.PruneAll {
		prune = "docker system prune -a -f"
	}
	if _, err := exec(ctx, nodeIP, guestShell(g, prune)); err != nil {
		r.fail("prune : %v", err)
		return r
	}
	unhealthy, err := exec(ctx, nodeIP, guestShell(g, "docker ps --filter health=unhealthy --format '{{.Names}}' || true"))
	if err != nil {
		r.fail("healthcheck : %v", err)
		return r
	}
	if !o.DryRun && strings.TrimSpace(unhealthy) != "" {
		r.fail("conteneurs unhealthy : %s", strings.TrimSpace(unhealthy))
		return r
	}
	r.Duration = time.Since(t0).Round(time.Second).String()
	return r
}
