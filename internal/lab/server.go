// Package lab est le cœur de Chili Lab : un petit
// serveur HTTP local (127.0.0.1) qui pilote la carte, et une page web ouverte
// dans une fenêtre d'application (Edge ou Chrome en mode --app).
//
// La page appelle l'API JSON (/api/…) avec un jeton aléatoire, et reçoit les
// événements en direct (console, état, progression du flash) par SSE.
package lab

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/getchili/chili/compiler/bytecode"
	"github.com/getchili/chili/compiler/codegen"
	"github.com/getchili/chili/compiler/device"
	"github.com/getchili/chili/compiler/diag"
	"github.com/getchili/chili/compiler/firmware"
)

//go:embed static
var static embed.FS

// Options de lancement.
type Options struct {
	Target   string // port ou adresse à utiliser ; "" = détection automatique
	Examples string // dossier des exemples ("" si absent)
	Version  string // version de Chili Lab
	NoWindow bool   // développement : affiche l'adresse au lieu d'ouvrir la fenêtre
}

type logLine struct {
	T    int64  `json:"t"`    // horodatage (ms)
	Text string `json:"text"` // contenu
	Kind string `json:"kind"` // out (sortie du programme), event (événement carte), sys (message de Chili Lab)
}

type event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Server est l'état de l'application.
type Server struct {
	opts  Options
	token string

	mu         sync.Mutex
	cfg        config
	conn       *device.Conn
	target     string
	info       device.Info
	heap       []int // historique de la mémoire libre
	busy       string
	manualOff  bool              // déconnecté à la main : pas de reconnexion auto
	noFirmware map[string]string // ports ESP32 sans firmware Chili -> modèle de puce
	ports      []device.Port

	evMu    sync.Mutex
	clients map[chan event]struct{}
	log     []logLine
	seen    bool      // au moins une fenêtre s'est connectée
	lastCli time.Time // dernière fois qu'une fenêtre était ouverte
}

// Run démarre le serveur, ouvre la fenêtre et rend la main quand elle est fermée.
//
// Une seule instance tourne à la fois (une carte ne s'ouvre qu'une fois) :
// si Chili Lab est déjà lancé, on ouvre simplement une fenêtre de plus.
func Run(opts Options) error {
	if url, ok := runningInstance(); ok {
		return openWindow(url)
	}

	tok := make([]byte, 16)
	rand.Read(tok)
	s := &Server{
		opts:       opts,
		token:      hex.EncodeToString(tok),
		cfg:        loadConfig(),
		clients:    map[chan event]struct{}{},
		noFirmware: map[string]string{},
		lastCli:    time.Now(),
	}

	seedProjects(s.cfg.ProjectsDir, opts.Examples)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s/?token=%s", ln.Addr(), s.token)
	registerInstance(url)
	defer unregisterInstance()

	go s.supervise()
	go http.Serve(ln, s.routes())

	if opts.NoWindow {
		fmt.Println(url)
	} else if err := openWindow(url); err != nil {
		return err
	}

	// Quitte quand aucune fenêtre n'est ouverte depuis un moment.
	for {
		time.Sleep(time.Second)
		s.evMu.Lock()
		idle := len(s.clients) == 0 && ((s.seen && time.Since(s.lastCli) > 4*time.Second) ||
			(!s.seen && time.Since(s.lastCli) > 60*time.Second))
		s.evMu.Unlock()
		if idle {
			s.disconnect(false)
			return nil
		}
	}
}

// ---------------------------------------------------------------------------
// Événements

func (s *Server) broadcast(typ string, data any) {
	s.evMu.Lock()
	defer s.evMu.Unlock()
	ev := event{typ, data}
	for ch := range s.clients {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (s *Server) logf(kind, format string, args ...any) {
	l := logLine{T: time.Now().UnixMilli(), Text: fmt.Sprintf(format, args...), Kind: kind}
	s.evMu.Lock()
	s.log = append(s.log, l)
	if len(s.log) > 2000 {
		s.log = s.log[len(s.log)-2000:]
	}
	s.evMu.Unlock()
	s.broadcast("line", l)
}

func (s *Server) toast(level, format string, args ...any) {
	s.broadcast("toast", map[string]string{"level": level, "msg": fmt.Sprintf(format, args...)})
}

// ---------------------------------------------------------------------------
// État

type portView struct {
	Name     string `json:"name"`
	Bridge   string `json:"bridge"`
	Likely   bool   `json:"likely"`
	Chip     string `json:"chip,omitempty"` // renseigné si la carte n'a pas le firmware Chili
	Selected bool   `json:"selected"`
}

// remoteView est une carte joignable par WiFi.
type remoteView struct {
	Target   string `json:"target"`
	Label    string `json:"label"`
	Selected bool   `json:"selected"`
}

type stateView struct {
	Connected bool              `json:"connected"`
	Target    string            `json:"target"`
	Info      map[string]string `json:"info"`
	Heap      []int             `json:"heap"`
	Busy      string            `json:"busy"`
	Ports     []portView        `json:"ports"`
	Remotes   []remoteView      `json:"remotes"`
	Version   string            `json:"version"`
	FWVersion string            `json:"fwVersion"`
	Bytecode  int               `json:"bytecode"`
}

func (s *Server) state() stateView {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := stateView{
		Connected: s.conn != nil,
		Target:    s.target,
		Info:      s.info,
		Heap:      append([]int(nil), s.heap...),
		Busy:      s.busy,
		Version:   s.opts.Version,
		FWVersion: firmware.Version(),
		Bytecode:  bytecode.Version,
	}
	for _, p := range s.ports {
		v.Ports = append(v.Ports, portView{
			Name: p.Name, Bridge: p.Bridge, Likely: p.Likely,
			Chip: s.noFirmware[p.Name], Selected: s.conn != nil && strings.EqualFold(p.Name, s.target),
		})
	}

	// Cartes WiFi : celles déjà utilisées, plus l'adresse WiFi de la carte
	// branchée en USB (pour basculer sans câble en un clic).
	seen := map[string]bool{}
	addRemote := func(target, label string) {
		if target == "" || seen[strings.ToLower(target)] {
			return
		}
		seen[strings.ToLower(target)] = true
		v.Remotes = append(v.Remotes, remoteView{target, label, s.conn != nil && strings.EqualFold(target, s.target)})
	}
	if s.conn != nil && !isSerial(s.target) {
		addRemote(s.target, "par WiFi")
	}
	if s.conn != nil && isSerial(s.target) && s.info["wifi"] == "connected" {
		addRemote(s.info["ip"], "cette carte, par WiFi")
	}
	for _, r := range s.cfg.Remotes {
		addRemote(r, "par WiFi")
	}
	return v
}

func (s *Server) pushState() { s.broadcast("state", s.state()) }

// supervise rafraîchit les ports, se (re)connecte et interroge la carte.
func (s *Server) supervise() {
	if s.opts.Target != "" {
		s.connect(s.opts.Target)
	}
	for i := 0; ; i++ {
		ports, _ := device.ListPorts()
		s.mu.Lock()
		s.ports = ports
		conn, busy, manual := s.conn, s.busy, s.manualOff
		s.mu.Unlock()

		switch {
		case busy != "":
		case conn != nil:
			if info, err := conn.Info(1500 * time.Millisecond); err == nil {
				s.mu.Lock()
				if s.conn == conn {
					s.info = info
					var heap int
					fmt.Sscan(info["heap"], &heap)
					s.heap = append(s.heap, heap)
					if len(s.heap) > 90 {
						s.heap = s.heap[len(s.heap)-90:]
					}
				}
				s.mu.Unlock()
			}
		case !manual && i%2 == 0:
			s.autoConnect(ports)
		}
		s.pushState()
		time.Sleep(2 * time.Second)
	}
}

// autoConnect essaie les ports qui ressemblent à un ESP32.
func (s *Server) autoConnect(ports []device.Port) {
	for _, p := range ports {
		if !p.Likely {
			continue
		}
		s.mu.Lock()
		_, known := s.noFirmware[p.Name]
		s.mu.Unlock()
		if known {
			continue // carte sans firmware déjà repérée : on attend un flash
		}
		if s.connect(p.Name) == nil {
			return
		}
	}
}

// connect ouvre la cible et vérifie que le firmware Chili répond.
func (s *Server) connect(target string) error {
	var conn *device.Conn
	var err error
	if isSerial(target) {
		conn, err = device.Open(target)
	} else {
		conn, err = device.Dial(target)
	}
	if err != nil {
		return err
	}
	info, err := conn.Info(2 * time.Second)
	if err != nil {
		conn.Close()
		if isSerial(target) && errors.Is(err, device.ErrNoFirmware) {
			// Une carte ESP32 sans Chili : on l'identifie pour proposer le flash.
			chip := "ESP32"
			if c, perr := device.ProbeBootloader(target, false); perr == nil {
				chip = c.Name
			}
			s.mu.Lock()
			s.noFirmware[target] = chip
			s.mu.Unlock()
			s.logf("sys", "%s : %s détecté, sans firmware Chili", target, chip)
		}
		return err
	}

	s.mu.Lock()
	old := s.conn
	s.conn, s.target, s.info, s.heap, s.manualOff = conn, target, info, nil, false
	delete(s.noFirmware, target)
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if !isSerial(target) {
		s.rememberRemote(target)
	}
	s.logf("sys", "Connecté à %s (firmware %s)", target, info["version"])
	go s.pump(conn)
	s.pushState()
	return nil
}

// rememberRemote mémorise une carte WiFi pour la proposer aux prochains lancements.
func (s *Server) rememberRemote(target string) {
	s.mu.Lock()
	list := []string{target}
	for _, r := range s.cfg.Remotes {
		if !strings.EqualFold(r, target) && len(list) < 8 {
			list = append(list, r)
		}
	}
	s.cfg.Remotes = list
	cfg := s.cfg
	s.mu.Unlock()
	saveConfig(cfg)
}

// pump relaie les lignes de la carte vers la console.
func (s *Server) pump(conn *device.Conn) {
	for line := range conn.Lines() {
		rest, ctrl := strings.CutPrefix(line, "!chili ")
		switch {
		case !ctrl:
			s.logf("out", "%s", line)
		case strings.HasPrefix(rest, "error "):
			s.logf("event", "Erreur : %s", strings.TrimPrefix(rest, "error "))
			s.toast("error", "Le programme s'est arrêté sur une erreur")
		case rest == "halt":
			s.logf("event", "Programme terminé")
		case strings.HasPrefix(rest, "boot"):
			s.logf("event", "La carte a redémarré")
		case strings.HasPrefix(rest, "wifi "):
			s.logf("event", "WiFi connecté : %s", strings.TrimPrefix(rest, "wifi "))
		}
	}
	s.mu.Lock()
	lost := s.conn == conn
	if lost {
		s.conn, s.info = nil, nil
	}
	busy := s.busy
	s.mu.Unlock()
	if lost && busy == "" {
		s.logf("sys", "Connexion perdue avec la carte")
		s.toast("warn", "Carte déconnectée")
		s.pushState()
	}
}

func (s *Server) disconnect(manual bool) {
	s.mu.Lock()
	conn := s.conn
	s.conn, s.info = nil, nil
	s.manualOff = manual
	s.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

func isSerial(t string) bool {
	u := strings.ToUpper(t)
	return strings.HasPrefix(u, "COM") || strings.HasPrefix(t, "/dev/")
}

// ---------------------------------------------------------------------------
// HTTP

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(static, "static")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/state", s.api(func(r *http.Request) (any, error) { return s.state(), nil }))
	mux.HandleFunc("/api/files", s.api(s.handleFiles))
	mux.HandleFunc("/api/connect", s.api(s.handleConnect))
	mux.HandleFunc("/api/disconnect", s.api(func(r *http.Request) (any, error) {
		s.disconnect(true)
		s.logf("sys", "Déconnecté")
		s.pushState()
		return nil, nil
	}))
	mux.HandleFunc("/api/cmd", s.api(s.handleCmd))
	mux.HandleFunc("/api/push", s.api(s.handlePush))
	mux.HandleFunc("/api/check", s.api(s.handleCheck))
	mux.HandleFunc("/api/wifi", s.api(s.handleWiFi))
	mux.HandleFunc("/api/flash", s.api(s.handleFlash))
	mux.HandleFunc("/api/dir", s.api(s.handleSetDir))
	mux.HandleFunc("/api/reveal", s.api(s.handleReveal))
	return mux
}

// apiError est une erreur renvoyée à la page avec un code HTTP et des détails.
type apiError struct {
	Status  int    `json:"-"`
	Message string `json:"error"`
	Details any    `json:"details,omitempty"`
}

func (e *apiError) Error() string { return e.Message }

func (s *Server) authorized(r *http.Request) bool {
	return r.Header.Get("X-Chili-Token") == s.token || r.URL.Query().Get("token") == s.token
}

func (s *Server) api(h func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			http.Error(w, "jeton invalide", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/api/state" && r.URL.Path != "/api/files" && r.Method != http.MethodPost {
			http.Error(w, "POST attendu", http.StatusMethodNotAllowed)
			return
		}
		res, err := h(r)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			var ae *apiError
			if !errors.As(err, &ae) {
				ae = &apiError{Status: http.StatusBadRequest, Message: err.Error()}
			}
			w.WriteHeader(ae.Status)
			json.NewEncoder(w).Encode(ae)
			return
		}
		if res == nil {
			res = map[string]bool{"ok": true}
		}
		json.NewEncoder(w).Encode(res)
	}
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "jeton invalide", http.StatusForbidden)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE non supporté", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	ch := make(chan event, 512)
	s.evMu.Lock()
	s.clients[ch] = struct{}{}
	s.seen = true
	backlog := append([]logLine(nil), s.log...)
	s.evMu.Unlock()
	defer func() {
		s.evMu.Lock()
		delete(s.clients, ch)
		s.lastCli = time.Now()
		s.evMu.Unlock()
	}()

	send := func(ev event) {
		b, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	send(event{"backlog", backlog})
	send(event{"state", s.state()})
	fl.Flush()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case ev := <-ch:
			send(ev)
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

type fileView struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Group string `json:"group"`
	Size  int64  `json:"size"`
	Mod   int64  `json:"mod"`
}

func (s *Server) handleFiles(r *http.Request) (any, error) {
	var files []fileView
	add := func(dir, group string) {
		if dir == "" {
			return
		}
		matches, _ := filepath.Glob(filepath.Join(dir, "*.chl"))
		sort.Strings(matches)
		for _, m := range matches {
			if st, err := os.Stat(m); err == nil {
				files = append(files, fileView{filepath.Base(m), m, group, st.Size(), st.ModTime().UnixMilli()})
			}
		}
	}
	s.mu.Lock()
	dir := s.cfg.ProjectsDir
	s.mu.Unlock()
	os.MkdirAll(dir, 0o755)
	add(dir, "Mes programmes")
	if s.opts.Examples != "" && !sameDir(s.opts.Examples, dir) {
		add(s.opts.Examples, "Exemples")
	}
	return map[string]any{"files": files, "workDir": dir}, nil
}

// handleSetDir change le dossier des programmes (mémorisé).
func (s *Server) handleSetDir(r *http.Request) (any, error) {
	var req struct{ Path string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	dir := strings.Trim(strings.TrimSpace(req.Path), `"`)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("dossier introuvable : %s", dir)
	}
	s.mu.Lock()
	s.cfg.ProjectsDir = dir
	cfg := s.cfg
	s.mu.Unlock()
	saveConfig(cfg)
	return nil, nil
}

// handleReveal ouvre le dossier des programmes dans l'Explorateur.
func (s *Server) handleReveal(r *http.Request) (any, error) {
	s.mu.Lock()
	dir := s.cfg.ProjectsDir
	s.mu.Unlock()
	os.MkdirAll(dir, 0o755)
	return nil, reveal(dir)
}

func sameDir(a, b string) bool {
	x, _ := filepath.Abs(a)
	y, _ := filepath.Abs(b)
	return strings.EqualFold(x, y)
}

func (s *Server) handleConnect(r *http.Request) (any, error) {
	var req struct{ Target string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		return nil, errors.New("indique un port (COM4) ou une adresse IP")
	}
	s.mu.Lock()
	delete(s.noFirmware, target)
	s.mu.Unlock()
	if err := s.connect(target); err != nil {
		if errors.Is(err, device.ErrNoFirmware) {
			return nil, fmt.Errorf("%s ne répond pas : le firmware Chili n'est pas installé ?", target)
		}
		return nil, fmt.Errorf("%s : %v", target, err)
	}
	return nil, nil
}

func (s *Server) currentConn() (*device.Conn, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy != "" {
		return nil, "", fmt.Errorf("opération en cours (%s)", s.busy)
	}
	if s.conn == nil {
		return nil, "", errors.New("aucune carte connectée")
	}
	return s.conn, s.target, nil
}

func (s *Server) handleCmd(r *http.Request) (any, error) {
	var req struct{ Cmd string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	msgs := map[string]string{
		"start": "Programme relancé",
		"stop":  "Programme en pause (repartira au prochain démarrage)",
		"kill":  "Programme arrêté (ne se relancera plus au démarrage)",
		"erase": "Programme effacé de la carte",
	}
	msg, ok := msgs[req.Cmd]
	if !ok {
		return nil, fmt.Errorf("commande inconnue : %s", req.Cmd)
	}
	conn, _, err := s.currentConn()
	if err != nil {
		return nil, err
	}
	if err := conn.Command(req.Cmd); err != nil {
		if strings.Contains(err.Error(), "commande inconnue") {
			return nil, errors.New("le firmware de la carte est trop ancien pour cette commande : mets-le à jour")
		}
		return nil, err
	}
	s.logf("sys", "%s", msg)
	s.refreshInfo(conn)
	return map[string]string{"message": msg}, nil
}

func (s *Server) refreshInfo(conn *device.Conn) {
	if info, err := conn.Info(1500 * time.Millisecond); err == nil {
		s.mu.Lock()
		if s.conn == conn {
			s.info = info
		}
		s.mu.Unlock()
	}
	s.pushState()
}

type compileErr struct {
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Msg  string `json:"msg"`
}

// compile lit le programme demandé (fichier du disque ou contenu envoyé).
func compile(path, name, source string) (*bytecode.Program, string, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", err
		}
		source, name = string(data), filepath.Base(path)
	}
	prog, err := codegen.Compile([]byte(source))
	if err != nil {
		var errs diag.ErrorList
		if errors.As(err, &errs) {
			var list []compileErr
			for _, e := range errs {
				list = append(list, compileErr{e.Pos.Line, e.Pos.Col, e.Msg})
			}
			return nil, name, &apiError{Status: http.StatusUnprocessableEntity,
				Message: fmt.Sprintf("%s contient %d erreur(s)", name, len(list)), Details: list}
		}
		return nil, name, err
	}
	return prog, name, nil
}

type pushReq struct {
	Path   string
	Name   string
	Source string
}

func (s *Server) handleCheck(r *http.Request) (any, error) {
	var req pushReq
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	prog, name, err := compile(req.Path, req.Name, req.Source)
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": name, "size": len(prog.Encode())}, nil
}

func (s *Server) handlePush(r *http.Request) (any, error) {
	var req pushReq
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	prog, name, err := compile(req.Path, req.Name, req.Source)
	if err != nil {
		return nil, err
	}
	conn, target, err := s.currentConn()
	if err != nil {
		return nil, err
	}
	data := prog.Encode()
	start := time.Now()
	if err := conn.Load(data); err != nil {
		return nil, fmt.Errorf("envoi refusé : %v", err)
	}
	ms := time.Since(start).Milliseconds()
	s.logf("sys", "%s envoyé sur %s (%d octets, %d ms) et lancé", name, target, len(data), ms)
	s.refreshInfo(conn)
	return map[string]any{"name": name, "size": len(data), "ms": ms}, nil
}

func (s *Server) handleWiFi(r *http.Request) (any, error) {
	var req struct{ SSID, Password string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	conn, _, err := s.currentConn()
	if err != nil {
		return nil, err
	}
	if err := conn.SetWiFi(req.SSID, req.Password); err != nil {
		return nil, err
	}
	if req.SSID == "" {
		s.logf("sys", "WiFi désactivé")
	} else {
		s.logf("sys", "Connexion au réseau « %s »…", req.SSID)
	}
	s.refreshInfo(conn)
	return nil, nil
}

func (s *Server) handleFlash(r *http.Request) (any, error) {
	var req struct{ Port string }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.busy != "" {
		s.mu.Unlock()
		return nil, fmt.Errorf("opération en cours (%s)", s.busy)
	}
	port := req.Port
	if port == "" && s.conn != nil && isSerial(s.target) {
		port = s.target
	}
	if port == "" || !isSerial(port) {
		s.mu.Unlock()
		return nil, errors.New("le firmware s'installe par USB : choisis le port de la carte")
	}
	s.busy = "flash"
	conn := s.conn
	s.conn, s.info = nil, nil
	s.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
	s.pushState()

	go func() {
		defer func() {
			s.mu.Lock()
			s.busy = ""
			s.mu.Unlock()
			s.pushState()
		}()
		var images []device.FlashImage
		for _, img := range firmware.ESP32() {
			images = append(images, device.FlashImage{Name: img.Name, Offset: img.Offset, Data: img.Data})
		}
		s.logf("sys", "Installation du firmware Chili %s sur %s…", firmware.Version(), port)
		last := -1
		err := device.Flash(port, false, "ESP32", images, func(name string, done, total int) {
			pct := done * 100 / total
			if pct != last {
				last = pct
				s.broadcast("flash", map[string]any{"percent": pct, "image": name})
			}
		})
		if err != nil {
			msg := err.Error()
			if errors.Is(err, device.ErrNoBootloader) {
				msg = "la carte ne passe pas en mode programmation : maintiens BOOT, appuie sur EN, relâche BOOT, puis réessaie"
			}
			s.logf("sys", "Échec du flash : %s", msg)
			s.broadcast("flash", map[string]any{"error": msg})
			return
		}
		s.logf("sys", "Firmware écrit et vérifié")
		s.broadcast("flash", map[string]any{"percent": 100, "done": true})
		s.mu.Lock()
		delete(s.noFirmware, port)
		s.mu.Unlock()
		time.Sleep(1500 * time.Millisecond)
		// busy reste posé jusqu'ici : la reconnexion automatique n'interfère pas.
		if err := s.connect(port); err != nil {
			s.logf("sys", "La carte ne répond pas encore : appuie sur EN (RST)")
		} else {
			s.toast("ok", "Firmware Chili installé")
		}
	}()
	return nil, nil
}
