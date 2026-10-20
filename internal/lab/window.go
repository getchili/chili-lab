package lab

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// openWindow ouvre url dans une fenêtre d'application sans barre d'adresse
// (Edge ou Chrome en mode --app), avec un profil dédié pour qu'elle ait sa
// propre icône dans la barre des tâches. À défaut, le navigateur par défaut.
func openWindow(url string) error {
	if runtime.GOOS != "windows" {
		for _, b := range []string{"google-chrome", "chromium", "microsoft-edge"} {
			if p, err := exec.LookPath(b); err == nil {
				return appWindow(p, url)
			}
		}
		return exec.Command("xdg-open", url).Start()
	}

	var candidates []string
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		candidates = append(candidates,
			filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(base, `Google\Chrome\Application\chrome.exe`))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return appWindow(c, url)
		}
	}
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err != nil {
		return errors.New("aucun navigateur trouvé")
	}
	return nil
}

func appWindow(browser, url string) error {
	profile := filepath.Join(os.TempDir(), "chili-lab")
	if dir, err := os.UserCacheDir(); err == nil {
		profile = filepath.Join(dir, "Chili Lab", "window")
	}
	cmd := exec.Command(browser,
		"--app="+url,
		"--user-data-dir="+profile,
		"--window-size=1380,900",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=Translate",
	)
	return cmd.Start()
}
