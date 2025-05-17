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
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", proxy.RemotePort))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", proxy.RemotePort, err)
	}

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
	msgBuf := make([]byte, 5)
	msgBuf[0] = tunnel.MsgTypeNewStream
	binary.BigEndian.PutUint32(msgBuf[1:], streamID)

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
