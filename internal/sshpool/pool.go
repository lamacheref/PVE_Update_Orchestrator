// Package sshpool maintient des connexions SSH persistantes vers les nodes.
//
// Principe : 1 *ssh.Client par node, conservé vivant (keepalive) et réutilisé
// pour chaque commande. Les guests sont atteints via pct/qm exec sur la
// connexion du node parent : pas de SSH direct vers chaque LXC/VM.
package sshpool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Options de connexion (une pool = une identité).
type Options struct {
	User           string
	Port           int
	KeyFile        string
	KnownHostsFile string
	DialTimeout    time.Duration
	CommandTimeout time.Duration
	Keepalive      time.Duration
	AcceptNewKeys  bool // bootstrap uniquement : ajoute les hôtes inconnus au known_hosts
}

func (o Options) withDefaults() Options {
	if o.Port == 0 {
		o.Port = 22
	}
	if o.DialTimeout == 0 {
		o.DialTimeout = 10 * time.Second
	}
	if o.CommandTimeout == 0 {
		o.CommandTimeout = 60 * time.Second
	}
	if o.Keepalive == 0 {
		o.Keepalive = 15 * time.Second
	}
	return o
}

// ExpandPath remplace ~ par $HOME.
func ExpandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func loadSigner(keyFile string) (ssh.Signer, error) {
	raw, err := os.ReadFile(ExpandPath(keyFile))
	if err != nil {
		return nil, fmt.Errorf("clé %s : %w", keyFile, err)
	}
	s, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("clé %s illisible : %w", keyFile, err)
	}
	return s, nil
}

func ensureFile(path string, perm os.FileMode) error {
	path = ExpandPath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	return f.Close()
}

// hostKeyCallback vérifie les clés d'hôte contre known_hosts.
// En mode AcceptNewKeys (bootstrap), un hôte inconnu est ajouté après
// journal de son empreinte ; tout mismatch ou révocation reste une erreur.
func hostKeyCallback(file string, acceptNew bool) (ssh.HostKeyCallback, error) {
	file = ExpandPath(file)
	if err := ensureFile(file, 0o600); err != nil {
		return nil, err
	}
	inner, err := knownhosts.New(file)
	if err != nil {
		return nil, fmt.Errorf("known_hosts %s : %w", file, err)
	}
	added := map[string]bool{}
	return func(host string, remote net.Addr, key ssh.PublicKey) error {
		if err := inner(host, remote, key); err == nil {
			return nil
		} else {
			var revoked *knownhosts.RevokedError
			if errors.As(err, &revoked) {
				return fmt.Errorf("clé d'hôte %s RÉVOQUÉE : %w", host, err)
			}
			var keyErr *knownhosts.KeyError
			if !errors.As(err, &keyErr) || len(keyErr.Want) > 0 {
				return fmt.Errorf("MISMATCH clé d'hôte %s (attaque ? known_hosts périmé ?) : %w", host, err)
			}
			if !acceptNew {
				return fmt.Errorf("hôte %s inconnu dans %s (passez par bootstrap-ssh) : %w", host, file, err)
			}
			id := host + "|" + string(key.Marshal())
			if !added[id] {
				addrs := []string{host}
				if n := knownhosts.Normalize(host); n != host {
					addrs = append(addrs, n)
				}
				f, ferr := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0o600)
				if ferr != nil {
					return ferr
				}
				if _, werr := fmt.Fprintln(f, knownhosts.Line(addrs, key)); werr != nil {
					f.Close()
					return werr
				}
				f.Close()
				added[id] = true
				fmt.Fprintf(os.Stderr, "🔑 hôte ajouté au known_hosts : %s (%s)\n", host, ssh.FingerprintSHA256(key))
			}
			if ni, nerr := knownhosts.New(file); nerr == nil {
				inner = ni
			}
			return nil
		}
	}, nil
}

type liveClient struct {
	c    *ssh.Client
	done chan struct{}
}

// Pool est sûre pour un usage concurrent.
type Pool struct {
	opts    Options
	signer  ssh.Signer
	hostKey ssh.HostKeyCallback
	mu      sync.Mutex
	clients map[string]*liveClient
}

// New charge la clé et prépare la pool (aucune connexion immédiate).
func New(opts Options) (*Pool, error) {
	opts = opts.withDefaults()
	signer, err := loadSigner(opts.KeyFile)
	if err != nil {
		return nil, err
	}
	cb, err := hostKeyCallback(opts.KnownHostsFile, opts.AcceptNewKeys)
	if err != nil {
		return nil, err
	}
	return &Pool{opts: opts, signer: signer, hostKey: cb, clients: map[string]*liveClient{}}, nil
}

func (p *Pool) dial(host string) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            p.opts.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(p.signer)},
		HostKeyCallback: p.hostKey,
		Timeout:         p.opts.DialTimeout,
	}
	addr := net.JoinHostPort(host, strconv.Itoa(p.opts.Port))
	c, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh %s@%s : %w", p.opts.User, addr, err)
	}
	return c, nil
}

// get rend la connexion persistante (sonde sans attente, re-dial si morte).
func (p *Pool) get(host string) (*ssh.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cl, ok := p.clients[host]; ok {
		_, _, err := cl.c.SendRequest("keepalive@openssh.com", false, nil)
		if err == nil {
			return cl.c, nil
		}
		cl.c.Close()
		close(cl.done)
		delete(p.clients, host)
	}
	c, err := p.dial(host)
	if err != nil {
		return nil, err
	}
	cl := &liveClient{c: c, done: make(chan struct{})}
	p.clients[host] = cl
	go keepalive(c, cl.done, p.opts.Keepalive)
	return c, nil
}

func (p *Pool) drop(host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cl, ok := p.clients[host]; ok {
		cl.c.Close()
		close(cl.done)
		delete(p.clients, host)
	}
}

func keepalive(c *ssh.Client, done <-chan struct{}, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			_, _, _ = c.SendRequest("keepalive@openssh.com", true, nil)
		}
	}
}

type result struct {
	out string
	err error
}

// Run exécute cmd sur host via la connexion persistante.
// En cas de session morte, re-dial + 1 seul nouvel essai.
func (p *Pool) Run(ctx context.Context, host, cmd string) (string, error) {
	c, err := p.get(host)
	if err != nil {
		return "", err
	}
	sess, err := c.NewSession()
	if err != nil {
		p.drop(host)
		if c, err = p.get(host); err != nil {
			return "", err
		}
		if sess, err = c.NewSession(); err != nil {
			return "", fmt.Errorf("session ssh %s : %w", host, err)
		}
	}
	defer sess.Close()

	ch := make(chan result, 1)
	go func() {
		out, err := sess.CombinedOutput(cmd)
		ch <- result{string(out), err}
	}()

	timeout := p.opts.CommandTimeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		if r.err != nil {
			return r.out, fmt.Errorf("ssh %s : %w (sortie : %s)", host, r.err, strings.TrimSpace(r.out))
		}
		return r.out, nil
	case <-ctx.Done():
		sess.Close()
		r := <-ch
		return r.out, fmt.Errorf("ssh %s annulé : %w", host, ctx.Err())
	case <-timer.C:
		sess.Close()
		r := <-ch
		return r.out, fmt.Errorf("ssh %s : timeout %s dépassé", host, timeout)
	}
}

// Close ferme toutes les connexions.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for h, cl := range p.clients {
		cl.c.Close()
		close(cl.done)
		delete(p.clients, h)
	}
}
