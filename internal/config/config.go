// Package config charge et valide la configuration (YAML, sans secrets).
//
// Les secrets transitent uniquement par variables d'environnement
// (ex. DISCORD_WEBHOOK_UPDATEUR), jamais par les fichiers versionnés.
package config

import (
	"fmt"
	"net"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration accepte "10s", "2m"… dans le YAML.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("durée %q invalide : %w", s, err)
	}
	*d = Duration(v)
	return nil
}

// Node décrit un hyperviseur du cluster.
type Node struct {
	Name string `yaml:"name"`
	IP   string `yaml:"ip"`
	Role string `yaml:"role"` // canary | standard
}

// NodesFile est le contenu de nodes.yaml.
type NodesFile struct {
	Nodes []Node `yaml:"nodes"`
}

// SSH regroupe les paramètres de connexion.
type SSH struct {
	User           string   `yaml:"user"`
	Port           int      `yaml:"port"`
	KeyFile        string   `yaml:"key_file"`
	KnownHosts     string   `yaml:"known_hosts"`
	DialTimeout    Duration `yaml:"dial_timeout"`
	CommandTimeout Duration `yaml:"command_timeout"`
	Keepalive      Duration `yaml:"keepalive"`
}

// Config est le contenu de config.yaml.
type Config struct {
	SSH         SSH `yaml:"ssh"`
	Concurrency struct {
		GuestWorkers int `yaml:"guest_workers"`
	} `yaml:"concurrency"`
	PBS struct {
		Storage string `yaml:"storage"`
		Mode    string `yaml:"mode"`
	} `yaml:"pbs"`
	Discord struct {
		WebhookEnv string `yaml:"webhook_env"`
	} `yaml:"discord"`
	Update struct {
		AutoReboot  bool `yaml:"auto_reboot"`
		KeepKernels int  `yaml:"keep_kernels"`
	} `yaml:"update"`
}

func readYAML(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("lecture %s : %w", path, err)
	}
	if err := yaml.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parse %s : %w", path, err)
	}
	return nil
}

// LoadNodes charge et valide nodes.yaml.
func LoadNodes(path string) ([]Node, error) {
	var f NodesFile
	if err := readYAML(path, &f); err != nil {
		return nil, err
	}
	if len(f.Nodes) == 0 {
		return nil, fmt.Errorf("%s : aucun node déclaré", path)
	}
	seen := map[string]bool{}
	for i, n := range f.Nodes {
		if n.Name == "" {
			return nil, fmt.Errorf("%s : node #%d sans nom", path, i)
		}
		if seen[n.Name] {
			return nil, fmt.Errorf("%s : node %q en double", path, n.Name)
		}
		seen[n.Name] = true
		if net.ParseIP(n.IP) == nil {
			return nil, fmt.Errorf("%s : node %q a une IP invalide %q", path, n.Name, n.IP)
		}
		if n.Role != "canary" && n.Role != "standard" {
			return nil, fmt.Errorf("%s : node %q a un rôle invalide %q (canary|standard)", path, n.Name, n.Role)
		}
	}
	return f.Nodes, nil
}

// LoadConfig charge et valide config.yaml.
func LoadConfig(path string) (Config, error) {
	var c Config
	if err := readYAML(path, &c); err != nil {
		return c, err
	}
	if c.SSH.User == "" {
		return c, fmt.Errorf("%s : ssh.user requis", path)
	}
	if c.SSH.Port < 1 || c.SSH.Port > 65535 {
		return c, fmt.Errorf("%s : ssh.port invalide", path)
	}
	if c.SSH.KeyFile == "" || c.SSH.KnownHosts == "" {
		return c, fmt.Errorf("%s : ssh.key_file et ssh.known_hosts requis", path)
	}
	if time.Duration(c.SSH.DialTimeout) <= 0 || time.Duration(c.SSH.CommandTimeout) <= 0 {
		return c, fmt.Errorf("%s : timeouts SSH strictement positifs requis", path)
	}
	if c.Concurrency.GuestWorkers < 1 {
		return c, fmt.Errorf("%s : concurrency.guest_workers >= 1 requis", path)
	}
	if c.PBS.Storage == "" || c.PBS.Mode != "snapshot" {
		return c, fmt.Errorf("%s : pbs.storage requis et pbs.mode=snapshot imposé", path)
	}
	if c.Discord.WebhookEnv == "" {
		return c, fmt.Errorf("%s : discord.webhook_env requis (nom de variable d'environnement)", path)
	}
	if c.Update.KeepKernels < 1 {
		return c, fmt.Errorf("%s : update.keep_kernels >= 1 requis", path)
	}
	return c, nil
}

// DiscordWebhook lit le webhook depuis l'environnement (jamais depuis git).
func (c Config) DiscordWebhook() (string, error) {
	v := os.Getenv(c.Discord.WebhookEnv)
	if v == "" {
		return "", fmt.Errorf("variable %s non définie (voir /etc/pve-orchestrator/.env)", c.Discord.WebhookEnv)
	}
	return v, nil
}

// FilterNodes ne garde que les nodes demandés (vide = tous).
func FilterNodes(nodes []Node, only map[string]bool) []Node {
	if len(only) == 0 {
		return nodes
	}
	var out []Node
	for _, n := range nodes {
		if only[n.Name] {
			out = append(out, n)
		}
	}
	return out
}
