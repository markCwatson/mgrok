package proxy

import (
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/xtaci/smux"
)

// ProxyInfo stores information about a registered proxy
type ProxyInfo struct {
	ProxyType  uint8
	LocalPort  uint16
	RemotePort uint16
	Name       string
	Listener   net.Listener // Only used for TCP proxies
	UDPConn    *net.UDPConn // Only used for UDP proxies
}

// ClientInfo stores information about a connected client
type ClientInfo struct {
	ID         string
	Conn       net.Conn
	Session    *smux.Session
	Proxies    map[string]*ProxyInfo
	CtrlStream *smux.Stream
	mu         sync.Mutex
}

// Manager manages all registered proxies
type Manager struct {
	clients     map[string]*ClientInfo
	portToProxy map[uint16]*ProxyInfo
	mu          sync.Mutex
}

// NewManager creates a new proxy manager
func NewManager() *Manager {
	return &Manager{
		clients:     make(map[string]*ClientInfo),
		portToProxy: make(map[uint16]*ProxyInfo),
	}
}

// AddClient adds a new client to the manager
func (m *Manager) AddClient(clientID string, session *smux.Session) *ClientInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	client := &ClientInfo{
		ID:      clientID,
		Session: session,
		Proxies: make(map[string]*ProxyInfo),
	}

	m.clients[clientID] = client
	return client
}

// RemoveClient removes a client and all its proxies
func (m *Manager) RemoveClient(clientID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, exists := m.clients[clientID]
	if !exists {
		return
	}

	// Clean up all listeners for this client
	for _, proxy := range client.Proxies {
		if proxy.Listener != nil {
			proxy.Listener.Close()
		}
		if proxy.UDPConn != nil {
			proxy.UDPConn.Close()
		}
		delete(m.portToProxy, proxy.RemotePort)
	}

	delete(m.clients, clientID)
	log.Printf("Client %s disconnected, cleaned up resources", clientID)
}

// GetClient gets a client by ID
func (m *Manager) GetClient(clientID string) *ClientInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.clients[clientID]
}

// IsPortAvailable checks if a port is available
func (m *Manager) IsPortAvailable(port uint16) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.portToProxy[port]
	return !exists
}

// RegisterProxy registers a new proxy
func (m *Manager) RegisterProxy(client *ClientInfo, name string, proxyType uint8, remotePort, localPort uint16) (*ProxyInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if port is already in use
	if _, exists := m.portToProxy[remotePort]; exists {
		return nil, fmt.Errorf("port %d already in use", remotePort)
	}

	// Create proxy info
	proxy := &ProxyInfo{
		ProxyType:  proxyType,
		LocalPort:  localPort,
		RemotePort: remotePort,
		Name:       name,
	}

	// Store the proxy
	client.mu.Lock()
	client.Proxies[name] = proxy
	client.mu.Unlock()

	m.portToProxy[remotePort] = proxy

	log.Printf("Registered proxy %s on port %d", name, remotePort)
	return proxy, nil
}

// closes all proxy TCP listeners
func (m *Manager) CloseAllListeners() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, client := range m.clients {
		for _, proxy := range client.Proxies {
			if proxy.Listener != nil {
				proxy.Listener.Close()
			}
			if proxy.UDPConn != nil {
				proxy.UDPConn.Close()
			}
		}
	}
}
