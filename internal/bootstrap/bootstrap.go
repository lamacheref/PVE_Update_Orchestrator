// Package bootstrap génère la clé SSH dédiée et la déploie sur les nodes
// via la clé seed (qui ne sert plus jamais ensuite).
package bootstrap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/lamacheref/pve-update-orchestrator/internal/sshpool"
)

// Runner exécute une commande sur un node (ex. méthode *sshpool.Pool.Run).
type Runner func(ctx context.Context, host, cmd string) (string, error)

// GenerateKey crée une ed25519 à path (+ .pub), ou relit l'existante.
// Retourne la ligne authorized_keys et created=vrai si nouvelle.
func GenerateKey(path string) (pubLine string, created bool, err error) {
	path = sshpool.ExpandPath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", false, err
	}
	if raw, rerr := os.ReadFile(path); rerr == nil {
		signer, perr := ssh.ParsePrivateKey(raw)
		if perr != nil {
			return "", false, fmt.Errorf("clé %s existante illisible : %w", path, perr)
		}
		return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), false, nil
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", false, err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", false, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return "", false, err
	}
	pubLine = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if err := os.WriteFile(path+".pub", []byte(pubLine+"\n"), 0o644); err != nil {
		return "", false, err
	}
	return pubLine, true, nil
}

// Fingerprint rend l'empreinte SHA256 d'une ligne authorized_keys.
func Fingerprint(pubLine string) (string, error) {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubLine))
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(key), nil
}

// EnsureAuthorizedKey ajoute pubLine à /root/.authorized_keys (idempotent).
func EnsureAuthorizedKey(ctx context.Context, run Runner, host, pubLine string) error {
	script := fmt.Sprintf(
		`mkdir -p /root/.ssh && chmod 700 /root/.ssh && touch /root/.ssh/authorized_keys && `+
			`grep -qxF %[1]q /root/.ssh/authorized_keys || echo %[1]q >> /root/.ssh/authorized_keys; `+
			`chmod 600 /root/.ssh/authorized_keys && echo DEPLOYED-OK`,
		pubLine,
	)
	out, err := run(ctx, host, script)
	if err != nil {
		return fmt.Errorf("déploiement clé sur %s : %w", host, err)
	}
	if !strings.Contains(out, "DEPLOYED-OK") {
		return fmt.Errorf("déploiement clé sur %s : sortie inattendue %q", host, out)
	}
	return nil
}
