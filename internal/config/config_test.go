package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadNodesOK(t *testing.T) {
	p := writeTemp(t, "nodes:\n  - {name: Janus, ip: 192.168.110.110, role: canary}\n  - {name: Zeus, ip: 192.168.110.106, role: standard}\n")
	nodes, err := LoadNodes(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].Role != "canary" {
		t.Fatalf("nodes inattendus : %+v", nodes)
	}
}

func TestLoadNodesKO(t *testing.T) {
	cases := []string{
		"nodes: []\n",
		"nodes:\n  - {name: A, ip: 999.1.1.1, role: standard}\n",
		"nodes:\n  - {name: A, ip: 192.168.1.1, role: standard}\n  - {name: A, ip: 192.168.1.2, role: standard}\n",
		"nodes:\n  - {name: A, ip: 192.168.1.1, role: bizarre}\n",
		"nodes: [oups\n",
	}
	for i, c := range cases {
		if _, err := LoadNodes(writeTemp(t, c)); err == nil {
			t.Errorf("cas %d : erreur attendue", i)
		}
	}
}

func TestLoadConfigExample(t *testing.T) {
	c, err := LoadConfig("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.SSH.User != "root" || c.PBS.Mode != "snapshot" || c.Update.KeepKernels != 2 {
		t.Fatalf("config exemple inattendue : %+v", c)
	}
}

func TestDiscordWebhookMissing(t *testing.T) {
	c, _ := LoadConfig("../../configs/config.example.yaml")
	t.Setenv(c.Discord.WebhookEnv, "")
	if _, err := c.DiscordWebhook(); err == nil {
		t.Error("erreur attendue quand la variable webhook est absente")
	}
}
