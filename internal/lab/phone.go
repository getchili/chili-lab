package lab

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	qrcode "github.com/skip2/go-qrcode"
)

// Accès depuis un téléphone : à la demande, Chili Lab écoute aussi sur
// l'adresse du PC dans le réseau local et affiche un QR code contenant
// l'adresse et le jeton. Sans le jeton, l'interface refuse tout. L'accès
// est coupé à la demande ou à la fermeture de Chili Lab.

type phoneAccess struct {
	mu  sync.Mutex
	ln  net.Listener
	url string
}

// lanIP renvoie l'adresse IPv4 du PC dans le réseau local (celle de la route
// par défaut ; aucun paquet n'est envoyé).
func lanIP() (net.IP, error) {
	c, err := net.Dial("udp4", "192.0.2.1:9") // adresse de documentation, jamais contactée
	if err != nil {
		return nil, errors.New("le PC n'est relié à aucun réseau")
	}
	defer c.Close()
	ip := c.LocalAddr().(*net.UDPAddr).IP
	if !ip.IsPrivate() {
		return nil, fmt.Errorf("adresse %s : ce n'est pas un réseau local (box, WiFi maison)", ip)
	}
	return ip, nil
}

type phoneView struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url,omitempty"`
	QR      string `json:"qr,omitempty"` // image PNG en data: URI
}

func (s *Server) phoneState() phoneView {
	s.phone.mu.Lock()
	defer s.phone.mu.Unlock()
	if s.phone.ln == nil {
		return phoneView{}
	}
	png, err := qrcode.Encode(s.phone.url, qrcode.Medium, 512)
	if err != nil {
		return phoneView{Enabled: true, URL: s.phone.url}
	}
	return phoneView{Enabled: true, URL: s.phone.url, QR: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)}
}

// handlePhone active ({"enable":true}) ou coupe l'accès téléphone.
func (s *Server) handlePhone(r *http.Request) (any, error) {
	var req struct{ Enable bool }
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if !req.Enable {
		s.stopPhone()
		s.logf("sys", "Accès téléphone coupé")
		return s.phoneState(), nil
	}

	s.phone.mu.Lock()
	running := s.phone.ln != nil
	s.phone.mu.Unlock()
	if !running {
		ip, err := lanIP()
		if err != nil {
			return nil, err
		}
		ln, err := net.Listen("tcp4", net.JoinHostPort(ip.String(), "0"))
		if err != nil {
			return nil, fmt.Errorf("impossible d'ouvrir l'accès réseau : %v", err)
		}
		url := fmt.Sprintf("http://%s/?token=%s", ln.Addr(), s.token)
		s.phone.mu.Lock()
		s.phone.ln, s.phone.url = ln, url
		s.phone.mu.Unlock()
		go http.Serve(ln, s.routes())
		s.logf("sys", "Accès téléphone ouvert sur %s", ln.Addr())
	}
	return s.phoneState(), nil
}

func (s *Server) stopPhone() {
	s.phone.mu.Lock()
	defer s.phone.mu.Unlock()
	if s.phone.ln != nil {
		s.phone.ln.Close()
		s.phone.ln, s.phone.url = nil, ""
	}
}
