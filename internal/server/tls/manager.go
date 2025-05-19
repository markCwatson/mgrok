package tls

import (
	"crypto/tls"
	"log"
	"net"

	"github.com/markCwatson/mgrok/internal/config"
)

type Manager struct {
	TLSCertFile string
	TLSKeyFile  string
	EnableTLS   bool
}

func NewManager(config *config.ServerConfig) *Manager {
	return &Manager{
		TLSCertFile: config.TLSCertFile,
		TLSKeyFile:  config.TLSKeyFile,
		EnableTLS:   config.EnableTLS,
	}
}

func (m *Manager) GetTLSConfig() *tls.Config {
	cert, err := tls.LoadX509KeyPair(m.TLSCertFile, m.TLSKeyFile)
	if err != nil {
		log.Fatalf("Failed to load TLS certificate: %v", err)
	}

	return &tls.Config{Certificates: []tls.Certificate{cert}}
}

// will fallback to plain TCP if TLS certificate and key files are not set
func (m *Manager) Listen(addr string) (net.Listener, error) {
	if m.EnableTLS {
		log.Printf("Listening on %s with TLS", addr)
		return tls.Listen("tcp", addr, m.GetTLSConfig())
	}

	log.Printf("Listening on %s without TLS", addr)
	return net.Listen("tcp", addr)
}
