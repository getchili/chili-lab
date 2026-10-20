package lab

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// config est ce que Chili Lab retient d'une fois sur l'autre.
type config struct {
	ProjectsDir string   `json:"projectsDir"` // dossier « Mes programmes »
	Remotes     []string `json:"remotes"`     // cartes déjà reliées par WiFi (IP ou nom)
}

// dataDir : %APPDATA%\Chili Lab (créé au besoin).
func dataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "Chili Lab")
	os.MkdirAll(dir, 0o755)
	return dir
}

func loadConfig() config {
	var c config
	if data, err := os.ReadFile(filepath.Join(dataDir(), "config.json")); err == nil {
		json.Unmarshal(data, &c)
	}
	if c.ProjectsDir == "" {
		home, _ := os.UserHomeDir()
		c.ProjectsDir = filepath.Join(home, "Documents", "Chili")
	}
	return c
}

func saveConfig(c config) {
	data, _ := json.MarshalIndent(c, "", "  ")
	os.WriteFile(filepath.Join(dataDir(), "config.json"), data, 0o644)
}

// ---------------------------------------------------------------------------
// Instance unique : l'adresse de l'instance en cours est notée dans un fichier.

func instanceFile() string { return filepath.Join(dataDir(), "instance.txt") }

func registerInstance(u string) { os.WriteFile(instanceFile(), []byte(u), 0o600) }

func unregisterInstance() { os.Remove(instanceFile()) }

// runningInstance renvoie l'adresse d'un Chili Lab déjà lancé, s'il répond.
func runningInstance() (string, bool) {
	data, err := os.ReadFile(instanceFile())
	if err != nil {
		return "", false
	}
	u, err := url.Parse(strings.TrimSpace(string(data)))
	if err != nil {
		return "", false
	}
	check := *u
	check.Path = "/api/state"
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(check.String())
	if err != nil {
		return "", false
	}
	resp.Body.Close()
	return u.String(), resp.StatusCode == http.StatusOK
}

// seedProjects crée le dossier des programmes au premier lancement et y copie
// les exemples, pour avoir tout de suite de quoi essayer et modifier.
func seedProjects(dir, examples string) {
	if _, err := os.Stat(dir); err == nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil || examples == "" {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(examples, "*.chl"))
	for _, m := range matches {
		if data, err := os.ReadFile(m); err == nil {
			os.WriteFile(filepath.Join(dir, filepath.Base(m)), data, 0o644)
		}
	}
}

// reveal ouvre un dossier dans l'Explorateur.
func reveal(dir string) error {
	return exec.Command("explorer", dir).Start()
}
