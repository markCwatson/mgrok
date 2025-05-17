package controller

import (
	"encoding/binary"
	"log"
	"net"

	"github.com/markCwatson/mgrok/internal/server/proxy"
	"github.com/markCwatson/mgrok/internal/tunnel"
	"github.com/xtaci/smux"
)

// Handler handles control connections
type Handler struct {
	proxyManager *proxy.Manager
}

// NewHandler creates a new control handler
func NewHandler(proxyManager *proxy.Manager) *Handler {
	return &Handler{
		proxyManager: proxyManager,
	}
}

// HandleConnection handles a control connection
func (h *Handler) HandleConnection(conn net.Conn, session *smux.Session, clientID string) {
	log.Printf("Control connection established for client %s", clientID)

	client := h.proxyManager.GetClient(clientID)
	if client == nil {
		log.Printf("Client %s not found", clientID)
		return
	}

	client.CtrlStream = conn.(*smux.Stream)
	buffer := make([]byte, 1024)

	n, err := conn.Read(buffer)
	if err != nil {
		log.Printf("Error reading handshake: %v", err)
		return
	}

	if n < 5 || string(buffer[:4]) != "GRT1" {
		log.Printf("Invalid handshake magic: %s", string(buffer[:4]))
		return
	}

	authMethod := buffer[4]
	log.Printf("Client using auth method: %d", authMethod)

	// For now, accept any auth (would verify token here in production)
	// AuthPayload would be buffer[5:n]

	// Process control messages
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			log.Printf("Control connection closed: %v", err)
			break
		}

		if n < 1 {
			continue
		}

		msgType := buffer[0]

		switch msgType {
		case tunnel.MsgTypeRegister:
			h.handleRegisterMsg(client, buffer[1:n])
		case tunnel.MsgTypeHeartbeat:
			// Echo back heartbeat
			_, _ = conn.Write([]byte{tunnel.MsgTypeHeartbeat})
		default:
			log.Printf("Unknown message type: %d", msgType)
		}
	}
}

// handleRegisterMsg handles a register message
func (h *Handler) handleRegisterMsg(client *proxy.ClientInfo, data []byte) {
	if len(data) < 5 { // proxyType(1) + remotePort(2) + localPort(2) + at least 1 byte name
		log.Printf("Register message too short")
		return
	}

	proxyType := data[0]
	remotePort := binary.BigEndian.Uint16(data[1:3])
	localPort := binary.BigEndian.Uint16(data[3:5])
	name := string(data[5:])

	log.Printf("Registration request: %s, type %d, remote port %d, local port %d",
		name, proxyType, remotePort, localPort)

	// Register proxy
	newProxy, err := h.proxyManager.RegisterProxy(client, name, proxyType, remotePort, localPort)
	if err != nil {
		log.Printf("Failed to register proxy: %v", err)
		// TODO: Send back error response
		return
	}

	// For TCP proxies, start a listener
	if proxyType == tunnel.ProxyTypeTCP {
		err = proxy.StartTCPListener(newProxy, client)
		if err != nil {
			log.Printf("Failed to start TCP listener: %v", err)
			// TODO: Send back error response
			return
		}
	}

	// TODO: Send back success response
}
