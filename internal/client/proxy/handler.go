package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/markCwatson/mgrok/internal/tunnel"
	"github.com/xtaci/smux"
)

// Handler handles client-side proxy connections
type Handler struct {
	session       *smux.Session
	config        *Config
	activeProxies map[string]*Proxy
}

// Config represents client configuration
type Config struct {
	Server  string `yaml:"server"`
	Token   string `yaml:"token"`
	Proxies map[string]struct {
		Type       string `yaml:"type"`
		LocalPort  int    `yaml:"local_port"`
		RemotePort int    `yaml:"remote_port"`
	} `yaml:"proxies"`
}

// Proxy represents a client-side proxy
type Proxy struct {
	Name       string
	Type       string
	LocalPort  int
	RemotePort int
	LocalConn  net.Conn
}

// NewHandler creates a new proxy handler
func NewHandler(session *smux.Session, config *Config) *Handler {
	return &Handler{
		session:       session,
		config:        config,
		activeProxies: make(map[string]*Proxy),
	}
}

// RegisterProxies registers all proxies with the server
func (h *Handler) RegisterProxies(conn net.Conn) {
	log.Println("Registering proxies...")

	// Write the protocol handshake
	if err := tunnel.WriteHandshake(conn, tunnel.AuthMethodToken, []byte(h.config.Token)); err != nil {
		log.Printf("Failed to write handshake: %v", err)
		return
	}

	// Register each proxy in the config
	for name, proxy := range h.config.Proxies {
		var proxyType uint8

		// Convert string type to uint8 protocol type
		switch proxy.Type {
		case "tcp":
			proxyType = tunnel.ProxyTypeTCP
		case "udp":
			proxyType = tunnel.ProxyTypeUDP
		default:
			log.Printf("Unknown proxy type for %s: %s", name, proxy.Type)
			continue
		}

		// Send registration message
		err := tunnel.WriteRegister(
			conn,
			proxyType,
			uint16(proxy.RemotePort),
			uint16(proxy.LocalPort),
			name,
		)

		if err != nil {
			log.Printf("Failed to register proxy %s: %v", name, err)
			continue
		}

		// Store the active proxy
		h.activeProxies[name] = &Proxy{
			Name:       name,
			Type:       proxy.Type,
			LocalPort:  proxy.LocalPort,
			RemotePort: proxy.RemotePort,
		}

		log.Printf("Registered proxy %s: %s port %d -> %d",
			name, proxy.Type, proxy.LocalPort, proxy.RemotePort)
	}
}

// HandleStream handles an incoming stream from the server
func (h *Handler) HandleStream(stream *smux.Stream) {
	defer stream.Close()

	// First byte should contain the message type
	msgTypeBuf := make([]byte, 1)
	if _, err := stream.Read(msgTypeBuf); err != nil {
		log.Printf("Failed to read message type: %v", err)
		return
	}

	msgType := msgTypeBuf[0]

	// Check if this is a NewStream message
	if msgType == tunnel.MsgTypeNewStream {
		h.handleNewStream(stream)
		return
	}

	log.Printf("Received unknown message type on stream %d: %d", stream.ID(), msgType)
	// Discard the rest of the data
	io.Copy(io.Discard, stream)
}

// handleNewStream handles a new stream request from the server
func (h *Handler) handleNewStream(stream *smux.Stream) {
	// Read the stream ID (uint32)
	streamIDBuf := make([]byte, 4)
	if _, err := io.ReadFull(stream, streamIDBuf); err != nil {
		log.Printf("Failed to read stream ID: %v", err)
		return
	}

	streamID := binary.BigEndian.Uint32(streamIDBuf)
	log.Printf("New stream request for ID %d", streamID)

	// Find the appropriate local port
	// In a more advanced implementation, the server would indicate which proxy this is for

	// Check if we have proxies
	if len(h.config.Proxies) == 0 {
		log.Printf("No proxies configured, cannot handle stream %d", streamID)
		stream.Close()
		return
	}

	// Get the first proxy (for demonstration purposes)
	var localPort int
	for _, proxy := range h.config.Proxies {
		localPort = proxy.LocalPort
		break
	}

	// Connect to the local service
	localAddr := fmt.Sprintf("localhost:%d", localPort)
	log.Printf("Connecting to local service at %s for stream %d", localAddr, streamID)

	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		log.Printf("Failed to connect to local service at %s: %v", localAddr, err)
		stream.Close()
		return
	}
	defer localConn.Close()

	// Set up bidirectional copy
	errCh := make(chan error, 2)

	// Copy from local service to remote
	go func() {
		_, err := io.Copy(stream, localConn)
		errCh <- err
	}()

	// Copy from remote to local service
	go func() {
		_, err := io.Copy(localConn, stream)
		errCh <- err
	}()

	// Wait for either copy to finish
	err = <-errCh
	if err != nil && err != io.EOF {
		log.Printf("Error in data forwarding: %v", err)
	}

	log.Printf("Stream %d closed", streamID)
}
