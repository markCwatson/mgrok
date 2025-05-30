package controller

import (
	"bytes"
	"encoding/binary"
	"log"

	"github.com/markCwatson/mgrok/internal/config"
	"github.com/markCwatson/mgrok/internal/server/proxy"
	"github.com/markCwatson/mgrok/internal/tunnel"
	"github.com/xtaci/smux"
)

// Handler handles control connections
type Handler struct {
	proxyManager *proxy.Manager
	serverConfig *config.ServerConfig
}

// NewHandler creates a new control handler
func NewHandler(proxyManager *proxy.Manager, serverConfig *config.ServerConfig) *Handler {
	return &Handler{
		proxyManager: proxyManager,
		serverConfig: serverConfig,
	}
}

// HandleConnection handles a control connection
func (h *Handler) HandleConnection(ctrlStream *smux.Stream, session *smux.Session, clientID string) {
	log.Printf("Control connection established for client %s", clientID)

	client := h.proxyManager.GetClient(clientID)
	if client == nil {
		log.Printf("Client %s not found", clientID)
		return
	}

	client.CtrlStream = ctrlStream
	buffer := make([]byte, 1024)

	// Read and validate handshake
	n, err := ctrlStream.Read(buffer)
	if err != nil {
		log.Printf("Error reading handshake: %v", err)
		return
	}

	if n > 5 {
		log.Printf("Handshake received (%d bytes): [% x] + auth payload (%d bytes)", 5, buffer[:5], n-5)
	} else {
		log.Printf("Handshake received (%d bytes): [% x]", n, buffer[:n])
	}

	// Check magic bytes - must be exactly "GRT1"
	magicBytes := []byte("GRT1")
	if n < 5 || !bytes.Equal(buffer[:4], magicBytes) {
		log.Printf("Invalid handshake magic: %q, expected: %q", string(buffer[:4]), string(magicBytes))
		return
	}

	authMethod := buffer[4]
	log.Printf("Client using auth method: %d", authMethod)

	if authMethod == tunnel.AuthMethodToken {
		if h.serverConfig.AuthToken == "" {
			log.Printf("Server auth token not configured, authentication failed")
			return
		}

		if n <= 5 {
			log.Printf("Authentication failed: No auth token provided")
			return
		}

		clientAuthToken := string(buffer[5:n])

		if clientAuthToken != h.serverConfig.AuthToken {
			log.Printf("Authentication failed: Invalid auth token")
			return
		}

		log.Printf("Authentication successful")
	} else {
		log.Printf("Unsupported auth method: %d", authMethod)
		return
	}

	// Process control messages
	for {
		n, err := ctrlStream.Read(buffer)
		if err != nil {
			log.Printf("Control connection closed: %v", err)
			break
		}

		if n < 1 {
			log.Printf("Received empty message, skipping")
			continue
		}

		// Dump raw message for debugging
		log.Printf("Received message (%d bytes): [% x]", n, buffer[:n])

		msgType := buffer[0]
		log.Printf("Message type: 0x%02x", msgType)

		switch msgType {
		case tunnel.MsgTypeRegister:
			h.handleRegisterMsg(client, buffer[1:n])
		case tunnel.MsgTypeHeartbeat:
			log.Printf("Received heartbeat")
			// Echo back heartbeat
			_, _ = ctrlStream.Write([]byte{tunnel.MsgTypeHeartbeat})
		default:
			log.Printf("Unknown message type: 0x%02x", msgType)
		}
	}
}

// handleRegisterMsg handles a register message
func (h *Handler) handleRegisterMsg(client *proxy.ClientInfo, data []byte) {
	log.Printf("Register message received (%d bytes): [% x]", len(data), data)

	if len(data) < 5 { // proxyType(1) + remotePort(2) + localPort(2) + at least 1 byte name
		log.Printf("Register message too short: expected at least 5 bytes, got %d", len(data))
		return
	}

	proxyType := data[0]
	remotePort := binary.BigEndian.Uint16(data[1:3])
	localPort := binary.BigEndian.Uint16(data[3:5])
	name := string(data[5:])

	log.Printf("Parsed registration request: %s, type=%d, remote_port=%d, local_port=%d",
		name, proxyType, remotePort, localPort)

	newProxy, err := h.proxyManager.RegisterProxy(client, name, proxyType, remotePort, localPort)
	if err != nil {
		log.Printf("Failed to register proxy: %v", err)
		// TODO: Send back error response
		return
	}

	switch proxyType {
	case tunnel.ProxyTypeTCP:
		err = proxy.StartTCPListener(newProxy, client)
		if err != nil {
			log.Printf("Failed to start TCP listener: %v", err)
			return
		}
	case tunnel.ProxyTypeUDP:
		err = proxy.StartUDPListener(newProxy, client)
		if err != nil {
			log.Printf("Failed to start UDP listener: %v", err)
			return
		}
	}

	// TODO: Send back success response
}
