package proxy

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/markCwatson/mgrok/internal/tunnel"
)

// StartTCPListener starts a TCP listener for a proxy
func StartTCPListener(proxy *ProxyInfo, client *ClientInfo) error {
	listenAddr := fmt.Sprintf(":%d", proxy.RemotePort)
	log.Printf("Starting TCP listener for proxy %s on %s", proxy.Name, listenAddr)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", proxy.RemotePort, err)
	}

	// Get the actual port in case the OS assigned one
	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		listener.Close()
		return fmt.Errorf("failed to parse listener address: %w", err)
	}

	// Confirm the listener is actually working
	log.Printf("TCP proxy %s successfully listening on port %s", proxy.Name, portStr)

	proxy.Listener = listener

	// Start accepting connections
	go acceptConnections(listener, client, proxy)

	return nil
}

// acceptConnections accepts connections on a TCP listener
func acceptConnections(listener net.Listener, client *ClientInfo, proxy *ProxyInfo) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Listener for proxy %s closed: %v", proxy.Name, err)
			break
		}

		log.Printf("New connection for proxy %s from %s", proxy.Name, conn.RemoteAddr())

		// Handle the connection in a goroutine
		go handleProxyConnection(conn, client, proxy)
	}
}

// handleProxyConnection handles a proxy connection
func handleProxyConnection(conn net.Conn, client *ClientInfo, proxy *ProxyInfo) {
	defer conn.Close()

	// Open a new stream to the client
	stream, err := client.Session.OpenStream()
	if err != nil {
		log.Printf("Failed to open stream to client: %v", err)
		return
	}
	defer stream.Close()

	// Send NewStream message with proxy identifier
	streamID := stream.ID()

	// Format:
	// 1. MsgTypeNewStream (1 byte)
	// 2. Stream ID (4 bytes)
	// 3. Remote port (2 bytes)
	// 4. Name length (1 byte)
	// 5. Proxy name (variable)

	nameBytes := []byte(proxy.Name)
	nameLen := len(nameBytes)

	// Allocate buffer for the entire message
	msgBuf := make([]byte, 8+nameLen)

	// 1. Message type
	msgBuf[0] = tunnel.MsgTypeNewStream

	// 2. Stream ID (4 bytes)
	binary.BigEndian.PutUint32(msgBuf[1:5], streamID)

	// 3. Remote port (2 bytes)
	binary.BigEndian.PutUint16(msgBuf[5:7], proxy.RemotePort)

	// 4. Name length (1 byte)
	msgBuf[7] = byte(nameLen)

	// 5. Proxy name (variable)
	if nameLen > 0 {
		copy(msgBuf[8:], nameBytes)
	}

	log.Printf("Sending NewStream for proxy %s (port %d), stream ID: %d",
		proxy.Name, proxy.RemotePort, streamID)

	// Send the complete message in one write
	_, err = stream.Write(msgBuf)
	if err != nil {
		log.Printf("Failed to send NewStream message: %v", err)
		return
	}

	// Now copy data in both directions
	go func() {
		_, _ = io.Copy(stream, conn)
		stream.Close()
	}()

	_, _ = io.Copy(conn, stream)
}
