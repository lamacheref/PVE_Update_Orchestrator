// Package discord envoie les rapports sur le canal bot-alarm via webhook.
//
// 1 message de démarrage + 1 embed par cible + 1 synthèse finale.
// Retry 3x, troncature aux limites Discord, jamais de secret en clair ici
// (l'URL vient de l'environnement, voir config.DiscordWebhook).
package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lamacheref/pve-update-orchestrator/internal/update"
)

const (
	ColorInfo    = 0x3498DB
	ColorSuccess = 0x2ECC71
	ColorWarn    = 0xF1C40F
	ColorFail    = 0xE74C3C
)

// Field est un champ d'embed.
type Field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// Embed est un embed Discord.
type Embed struct {
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description,omitempty"`
	Color       int     `json:"color,omitempty"`
	Fields      []Field `json:"fields,omitempty"`
}

// Message est un payload webhook.
type Message struct {
	Content string  `json:"content,omitempty"`
	Embeds  []Embed `json:"embeds,omitempty"`
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Send poste le message avec retry (3x, backoff).
func Send(client *http.Client, webhookURL string, msg Message) error {
	if webhookURL == "" {
		return fmt.Errorf("webhook vide")
	}
	for _, e := range msg.Embeds {
		e.Description = truncate(e.Description, 4000)
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	var last error
	for i := 0; i < 3; i++ {
		req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			last = err
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		if resp.StatusCode == 429 {
			time.Sleep(5 * time.Second)
			last = fmt.Errorf("discord 429 rate-limit")
			continue
		}
		return fmt.Errorf("discord HTTP %d", resp.StatusCode)
	}
	return fmt.Errorf("discord après 3 essais : %w", last)
}

// StartMessage annonce le run.
func StartMessage(runID, scope string, dryRun bool) Message {
	mode := "🚀 LIVE"
	if dryRun {
		mode = "🔍 DRY-RUN"
	}
	return Message{
		Content: fmt.Sprintf("%s **run `%s`** — scope : %s", mode, runID, scope),
	}
}

// TargetEmbed rend 1 embed par cible depuis un Result.
func TargetEmbed(r update.Result) Embed {
	color := ColorSuccess
	icon := "🟢"
	if !r.OK {
		color = ColorFail
		icon = "🔴"
	} else if r.Error == "" && (r.KernelBefore != r.KernelAfter && r.KernelAfter != "") {
		// succès avec reboot : reste vert, détail dans les champs.
	}
	title := fmt.Sprintf("%s %s", icon, r.Target)
	if r.DryRun {
		title += " [dry-run]"
	}
	desc := ""
	if !r.OK {
		desc = "❌ " + truncate(r.Error, 500)
	} else {
		desc = fmt.Sprintf("pkgs %d→%d • kernel %s→%s • %s",
			r.PkgsBefore, r.PkgsAfter, shortKernel(r.KernelBefore), shortKernel(r.KernelAfter), r.Duration)
		if r.Rebooted {
			desc += " • 🔁 rebooté"
		}
	}
	e := Embed{Title: title, Description: truncate(desc, 1000), Color: color}
	if r.BackupID != "" {
		e.Fields = append(e.Fields, Field{Name: "💾 Backup", Value: truncate(r.BackupID, 200), Inline: true})
	}
	if len(r.Log) > 0 {
		tail := r.Log
		if len(tail) > 4 {
			tail = tail[len(tail)-4:]
		}
		e.Fields = append(e.Fields, Field{Name: "📝 Log", Value: truncate("```\n"+strings.Join(tail, "\n")+"\n```", 900)})
	}
	if !r.OK && r.BackupID != "" {
		e.Fields = append(e.Fields, Field{Name: "↩️ Rollback", Value: fmt.Sprintf("`rollback --run-id <id> --target %s` (snapshot %s)", r.Target, truncate(r.BackupID, 100))})
	}
	return e
}

func shortKernel(k string) string {
	if k == "" {
		return "?"
	}
	if len(k) > 18 {
		return k[:18] + "…"
	}
	return k
}

// SummaryMessage rend la synthèse finale.
func SummaryMessage(runID string, results []update.Result, total time.Duration) Message {
	ok, fail := 0, 0
	var failed []string
	var actions []string
	for _, r := range results {
		if r.OK {
			ok++
		} else {
			fail++
			failed = append(failed, r.Target)
			if r.BackupID != "" {
				actions = append(actions, fmt.Sprintf("↩️ `%s` (snapshot %s)", r.Target, r.BackupID))
			} else {
				actions = append(actions, fmt.Sprintf("⚠️ `%s` : %s", r.Target, truncate(r.Error, 150)))
			}
		}
	}
	color := ColorSuccess
	head := fmt.Sprintf("🏁 **run `%s` terminé : %d OK / %d FAIL en %s**", runID, ok, fail, total.Round(time.Second))
	if fail > 0 {
		color = ColorFail
	}
	desc := head
	if len(failed) > 0 {
		desc += "\n❌ " + strings.Join(failed, ", ")
	}
	if len(actions) > 0 {
		desc += "\n\n**Actions requises :**\n" + strings.Join(actions, "\n")
	}
	return Message{Embeds: []Embed{{Title: "Synthèse " + runID, Description: truncate(desc, 3500), Color: color}}}
}
