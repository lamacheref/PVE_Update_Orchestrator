package cluster

import (
	"context"
	"errors"
	"testing"

	"github.com/lamacheref/pve-update-orchestrator/internal/config"
)

const pvecmNodesSample = `Membership information
----------------------
    Nodeid      Votes Name
         1          1 Janus (local)
         2          1 Zeus
         3          1 Nyx
`

func TestParseNodes(t *testing.T) {
	nodes, err := ParseNodes(pvecmNodesSample)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 || nodes[0].Name != "Janus" || nodes[0].NodeID != 1 {
		t.Fatalf("parse inattendu : %+v", nodes)
	}
	if _, err := ParseNodes("vide\n"); err == nil {
		t.Error("erreur attendue sur sortie vide")
	}
}

type fakeRun struct{}

func (fakeRun) Run(_ context.Context, host, cmd string) (string, error) {
	switch {
	case cmd == "pvecm nodes":
		return pvecmNodesSample, nil
	case len(cmd) > 13 && cmd[:13] == "getent hosts ":
		return "192.168.110.200\n", nil
	}
	return "", errors.New("cmd inconnue")
}

func TestDiscoverResolve(t *testing.T) {
	ctx := context.Background()
	nodes, err := Discover(ctx, fakeRun{}.Run, "seed")
	if err != nil {
		t.Fatal(err)
	}
	if errs := ResolveAll(ctx, fakeRun{}.Run, "seed", nodes); len(errs) != 0 {
		t.Fatal(errs)
	}
	if nodes[1].IP != "192.168.110.200" {
		t.Fatalf("IP non résolue : %+v", nodes[1])
	}
}

func TestReconcile(t *testing.T) {
	file := []config.Node{
		{Name: "Janus", IP: "192.168.110.110", Role: "canary"},
		{Name: "Vieux", IP: "192.168.110.99", Role: "standard"},
	}
	live := []Discovered{{Name: "janus"}, {Name: "Zeus"}}
	p := Reconcile(file, live)
	if len(p.Matched) != 1 || p.Matched[0].File.Name != "Janus" {
		t.Fatalf("match insensible à la casse attendu : %+v", p.Matched)
	}
	if len(p.Matched) != 1 || len(p.New) != 1 || p.New[0].Name != "Zeus" {
		t.Fatalf("plan inattendu : %+v", p)
	}
	if len(p.Gone) != 1 || p.Gone[0].Name != "Vieux" {
		t.Fatalf("gone inattendu : %+v", p.Gone)
	}
}
