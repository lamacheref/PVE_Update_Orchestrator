package bootstrap

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test_ed25519")
	pub1, created, err := GenerateKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if !created || !strings.HasPrefix(pub1, "ssh-ed25519 ") {
		t.Fatalf("clé inattendue : created=%v pub=%q", created, pub1)
	}
	pub2, created, err := GenerateKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if created || pub1 != pub2 {
		t.Error("la 2e génération doit relire la clé existante")
	}
	if fp, err := Fingerprint(pub1); err != nil || !strings.HasPrefix(fp, "SHA256:") {
		t.Errorf("empreinte inattendue : %q %v", fp, err)
	}
}
