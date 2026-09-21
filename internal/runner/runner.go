// Package runner orchestre un run complet : rolling 1 node à la fois,
// guests en parallèle bornée, pré/post-checks, backup fail-closed,
// Discord best-effort, état persistant pour --resume, verrou global.
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lamacheref/pve-update-orchestrator/internal/config"
	"github.com/lamacheref/pve-update-orchestrator/internal/discord"
	"github.com/lamacheref/pve-update-orchestrator/internal/health"
	"github.com/lamacheref/pve-update-orchestrator/internal/inventory"
	"github.com/lamacheref/pve-update-orchestrator/internal/update"
)

// Runner exécute une commande sur un hôte.
type Runner func(ctx context.Context, host, cmd string) (string, error)

// Options du run.
type Options struct {
	RunID         string
	NodeFilter    map[string]bool
	VMIDs         map[int]bool
	Only          string // "" | "os" | "docker"
	Skip          map[string]bool
	DryRun        bool
	AutoReboot    bool
	CanaryFirst   bool
	Workers       int
	PBSStorage    string
	KeepKernels   int
	PruneAll      bool
	BackupTimeout time.Duration // 0 = suivi illimité
	BackupWait    time.Duration // attente max backup tiers (<=0 = report immédiat)
	StateDir      string
	Resume        string // run-id à reprendre ("" = nouveau run)
	WebhookURL    string // "" = Discord désactivé (log stderr)
}

// TargetResult enrichit update.Result des checks.
type TargetResult struct {
	update.Result
	Pre  []health.Check `json:"pre_checks"`
	Post []health.Check `json:"post_checks,omitempty"`
	Hold bool           `json:"hold,omitempty"`
}

// RunState est persisté en JSON pour --resume et le dashboard.
type RunState struct {
	RunID     string                  `json:"run_id"`
	StartedAt string                  `json:"started_at"`
	DryRun    bool                    `json:"dry_run"`
	Targets   map[string]TargetResult `json:"targets"`
}

func newRunID() string { return time.Now().Format("20060102-1504") }

// orderNodes met le canary en premier si demandé.
func orderNodes(nodes []config.Node, canaryFirst bool) []config.Node {
	if !canaryFirst {
		return nodes
	}
	out := append([]config.Node{}, nodes...)
	sort.SliceStable(out, func(i, j int) bool {
		ci := out[i].Role == "canary"
		cj := out[j].Role == "canary"
		return ci && !cj
	})
	return out
}

type notifier struct {
	url string
	dry bool
}

func (n notifier) send(msg discord.Message, what string) {
	if n.url == "" {
		return // Discord désactivé : silencieux côté run, visible via --dry-run plan
	}
	if err := discord.Send(nil, n.url, msg); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️ discord %s : %v\n", what, err)
	}
}

func scopeString(o Options) string {
	var parts []string
	if len(o.NodeFilter) > 0 {
		var n []string
		for k := range o.NodeFilter {
			n = append(n, k)
		}
		sort.Strings(n)
		parts = append(parts, "nodes="+strings.Join(n, ","))
	} else {
		parts = append(parts, "nodes=all")
	}
	if len(o.VMIDs) > 0 {
		parts = append(parts, "vmids=filter")
	}
	if o.Only != "" {
		parts = append(parts, "only="+o.Only)
	}
	parts = append(parts, fmt.Sprintf("reboot=%v", o.AutoReboot))
	return strings.Join(parts, " ")
}

// Run exécute l'orchestration complète.
func Run(ctx context.Context, run Runner, nodes []config.Node, o Options) (*RunState, error) {
	if o.RunID == "" {
		o.RunID = newRunID()
	}
	if o.Workers < 1 {
		o.Workers = 4
	}
	if o.StateDir == "" {
		o.StateDir = "state"
	}
	if err := os.MkdirAll(o.StateDir, 0o755); err != nil {
		return nil, err
	}
	unlock, err := lock(o.StateDir)
	if err != nil {
		return nil, err
	}
	defer unlock()

	done := map[string]bool{} // cibles OK à sauter en --resume
	st := &RunState{RunID: o.RunID, StartedAt: time.Now().Format(time.RFC3339), DryRun: o.DryRun, Targets: map[string]TargetResult{}}
	if o.Resume != "" {
		prev, err := loadState(o.StateDir, o.Resume)
		if err != nil {
			return nil, fmt.Errorf("resume %s : %w", o.Resume, err)
		}
		st.RunID = prev.RunID
		for k, v := range prev.Targets {
			st.Targets[k] = v
			if v.OK && !v.Hold {
				done[k] = true // holds/skips rejoués au resume (cause peut-être réglée)
			}
		}
		fmt.Fprintf(os.Stderr, "⏯️ reprise %s : %d cible(s) déjà OK\n", o.Resume, len(done))
	}

	ntf := notifier{url: o.WebhookURL, dry: o.DryRun}
	ntf.send(discord.StartMessage(st.RunID, scopeString(o), o.DryRun), "start")
	t0 := time.Now()
	save := func() {
		if err := saveState(o.StateDir, st); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️ état : %v\n", err)
		}
	}

	nodes = config.FilterNodes(nodes, o.NodeFilter)
	nodes = orderNodes(nodes, o.CanaryFirst)
	uo := update.Options{DryRun: o.DryRun, AutoReboot: o.AutoReboot, KeepKernels: o.KeepKernels,
		PBSStorage: o.PBSStorage, PruneAll: o.PruneAll,
		BackupTimeout: o.BackupTimeout, BackupWait: o.BackupWait}

	// Inventaire guests (lecture seule, même en dry-run).
	var guests []inventory.Guest
	infos := inventory.Sync(ctx, inventory.Runner(run), nodes, o.Workers)
	for _, in := range infos {
		guests = append(guests, in.Guests...)
	}
	byNode := map[string][]inventory.Guest{}
	for _, g := range guests {
		byNode[g.Node] = append(byNode[g.Node], g)
	}

	emit := func(tr TargetResult) {
		st.Targets[tr.Target] = tr
		save()
		status := "✅"
		if !tr.OK {
			status = "❌"
		} else if tr.Hold {
			status = "⏸️"
		}
		fmt.Fprintf(os.Stderr, "%s %s\n", status, tr.Target)
		ntf.send(discord.Message{Embeds: []discord.Embed{discord.TargetEmbed(tr.Result)}}, tr.Target)
	}

	for _, n := range nodes {
		nodeTarget := "node/" + n.Name
		held := false
		switch {
		case done[nodeTarget]:
			fmt.Fprintf(os.Stderr, "⏭️ %s déjà OK (resume)\n", nodeTarget)
		case o.Skip[nodeTarget]:
			emit(TargetResult{Result: update.Result{Target: nodeTarget, Kind: "node", OK: true, DryRun: o.DryRun, Error: "skippé", Duration: "0s"}, Hold: true})
			held = true
		default:
			pre := health.CheckNode(ctx, health.Runner(run), n.IP)
			if health.Critical(pre) {
				var causes []string
				for _, c := range pre {
					if !c.OK {
						causes = append(causes, c.Name+": "+c.Detail)
					}
				}
				emit(TargetResult{Result: update.Result{Target: nodeTarget, Kind: "node", DryRun: o.DryRun,
					Error: "HOLD pré-check : " + strings.Join(causes, " | ")}, Pre: pre, Hold: true})
				held = true
				break
			}
			res := update.UpdateNode(ctx, update.Runner(run), n.IP, n.Name, uo)
			post := health.CheckNode(ctx, health.Runner(run), n.IP)
			emit(TargetResult{Result: res, Pre: pre, Post: post})
		}
		// Guests du node en parallèle bornée.
		var gsel []inventory.Guest
		for _, g := range byNode[n.Name] {
			id := fmt.Sprintf("%s/%d", g.Kind, g.VMID)
			target := fmt.Sprintf("%s/%d@%s", g.Kind, g.VMID, g.Name)
			if done[target] {
				fmt.Fprintf(os.Stderr, "⏭️ %s déjà OK (resume)\n", target)
				continue
			}
			if held {
				emit(TargetResult{Result: update.Result{Target: target, Kind: g.Kind, OK: true, DryRun: o.DryRun, Error: "skippé (node en hold)", Duration: "0s"}, Hold: true})
				continue
			}
			if len(o.VMIDs) > 0 && !o.VMIDs[g.VMID] {
				continue
			}
			if o.Skip[id] || o.Skip[target] {
				continue
			}
			gsel = append(gsel, g)
		}
		sort.Slice(gsel, func(i, j int) bool { return gsel[i].VMID < gsel[j].VMID })
		var wg sync.WaitGroup
		sem := make(chan struct{}, o.Workers)
		var mu sync.Mutex
		for _, g := range gsel {
			wg.Add(1)
			go func(g inventory.Guest) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				tr := runGuest(ctx, run, n.IP, g, uo, st.RunID, o)
				mu.Lock()
				emit(tr)
				mu.Unlock()
			}(g)
		}
		wg.Wait()
	}

	var results []update.Result
	for _, tr := range st.Targets {
		results = append(results, tr.Result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Target < results[j].Target })
	ntf.send(discord.SummaryMessage(st.RunID, results, time.Since(t0)), "synthèse")
	save()
	return st, nil
}

func runGuest(ctx context.Context, run Runner, nodeIP string, g inventory.Guest, uo update.Options, runID string, o Options) TargetResult {
	target := fmt.Sprintf("%s/%d@%s", g.Kind, g.VMID, g.Name)
	if o.Only == "docker" && !update.HasDocker(ctx, update.Runner(run), nodeIP, g, uo.DryRun) {
		return TargetResult{Result: update.Result{Target: target, Kind: g.Kind, OK: true, DryRun: uo.DryRun,
			Error: "skippé (pas de docker)", Duration: "0s"}, Hold: true}
	}
	pre := health.CheckGuest(ctx, health.Runner(run), nodeIP, g)
	res := update.UpdateGuest(ctx, update.Runner(run), nodeIP, g, uo, runID)
	if res.Skipped {
		return TargetResult{Result: res, Pre: pre, Hold: true}
	}
	if res.OK && (o.Only == "" || o.Only == "docker") {
		dres := update.UpdateDocker(ctx, update.Runner(run), nodeIP, g, uo)
		res.Log = append(res.Log, dres.Log...)
		if !dres.OK {
			res.OK = false
			res.Error = "docker : " + dres.Error
		}
	}
	post := health.CheckGuest(ctx, health.Runner(run), nodeIP, g)
	return TargetResult{Result: res, Pre: pre, Post: post}
}

func saveState(dir string, st *RunState) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, st.RunID+".json.tmp")
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, st.RunID+".json"))
}

func loadState(dir, runID string) (*RunState, error) {
	raw, err := os.ReadFile(filepath.Join(dir, runID+".json"))
	if err != nil {
		return nil, err
	}
	var st RunState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// lock pose un verrou global non bloquant (flock).
func lock(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, "pve-orchestrator.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("un run est déjà en cours (lock) : %w", err)
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
