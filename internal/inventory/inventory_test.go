package inventory

import (
	"context"
	"errors"
	"testing"

	"github.com/lamacheref/pve-update-orchestrator/internal/config"
)

const pvecmSample = `Cluster information
-------------------
Name:             homelab
Config Version:   7
Transport:        knet

Quorum information
------------------
Date:             Sun Sep 21 11:00:00 2026
Quorum provider:  corosync_votequorum
Nodes:            7
Node ID:          0x00000001
Ring ID:          1.42
Quorate:          Yes
`

const pctSample = `VMID       Status     Lock         Name
100        running                 docker-1
101        stopped                 test-lxc
`

const qmSample = `      VMID NAME                 STATUS     MEM(MB)    BOOTDISK(GB)  PID
       200 vm-web               running    4096              32.00 1234
       201 vm-db                stopped    2048              20.00 0
`

func TestParseQuorum(t *testing.T) {
	if !ParseQuorum(pvecmSample) {
		t.Error("quorum attendu à Yes")
	}
	if ParseQuorum("Quorate:          No\n") {
		t.Error("quorum No attendu")
	}
	if !ParseQuorum("Quorum: Yes\n") {
		t.Error("forme Quorum: acceptée aussi")
	}
	if ParseQuorum("vide") {
		t.Error("faux positif sur sortie vide")
	}
	if n := ParseNodeCount(pvecmSample); n != 7 {
		t.Errorf("node_count = %d, attendu 7", n)
	}
}

func TestParseGuests(t *testing.T) {
	lxc, _ := ParsePCT("Janus", pctSample)
	if len(lxc) != 2 || lxc[0].VMID != 100 || lxc[0].Name != "docker-1" || lxc[0].Kind != "lxc" {
		t.Fatalf("pct inattendu : %+v", lxc)
	}
	if lxc[1].Status != "stopped" {
		t.Fatalf("statut pct inattendu : %+v", lxc[1])
	}
	qemu, _ := ParseQM("Janus", qmSample)
	if len(qemu) != 2 || qemu[0].VMID != 200 || qemu[0].Name != "vm-web" || qemu[0].Kind != "qemu" {
		t.Fatalf("qm inattendu : %+v", qemu)
	}
}

type fakeRunner struct {
	fail map[string]bool
}

func (f fakeRunner) Run(_ context.Context, host, cmd string) (string, error) {
	if f.fail[host+cmd] {
		return "", errors.New("boom")
	}
	switch cmd {
	case "pvecm status":
		return pvecmSample, nil
	case "uname -r":
		return "6.8.12-4-pve\n", nil
	case "pveversion | head -1":
		return "pve-manager/8.2.2/9355359cd7afbae4 (running kernel: 6.8.12-4-pve)\n", nil
	case "pct list":
		return pctSample, nil
	case "qm list":
		return qmSample, nil
	}
	return "", errors.New("cmd inconnue")
}

func TestSyncOK(t *testing.T) {
	nodes := []config.Node{{Name: "Janus", IP: "192.168.110.110", Role: "canary"}}
	infos := Sync(context.Background(), fakeRunner{}.Run, nodes, 2)
	if len(infos) != 1 {
		t.Fatalf("infos : %+v", infos)
	}
	in := infos[0]
	if !in.Quorate || in.Kernel != "6.8.12-4-pve" || len(in.Guests) != 4 {
		t.Fatalf("sync inattendu : %+v", in)
	}
	if HasErrors(infos) {
		t.Fatalf("erreurs inattendues : %+v", in.Errors)
	}
}

func TestSyncPartialFailure(t *testing.T) {
	nodes := []config.Node{
		{Name: "Janus", IP: "10.0.0.1", Role: "canary"},
		{Name: "Zeus", IP: "10.0.0.2", Role: "standard"},
	}
	r := fakeRunner{fail: map[string]bool{"10.0.0.2pvecm status": true, "10.0.0.2uname -r": true}}
	infos := Sync(context.Background(), r.Run, nodes, 2)
	if !HasErrors(infos) || len(infos[0].Errors) != 0 || len(infos[1].Errors) == 0 {
		t.Fatalf("panne partielle mal gérée : %+v", infos)
	}
	if len(infos[0].Guests) != 4 {
		t.Fatalf("le node sain doit être complet : %+v", infos[0])
	}
}
