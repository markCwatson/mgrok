package proxy

import (
	"encoding/binary"
	"io"
	"log"
	"net"

	"github.com/markCwatson/mgrok/internal/tunnel"
)

// StartUDPListener starts a UDP listener for a proxy
func StartUDPListener(proxy *ProxyInfo, client *ClientInfo) error {
	addr := net.UDPAddr{Port: int(proxy.RemotePort)}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return err
	}
	proxy.UDPConn = conn
	go acceptUDPPackets(conn, client, proxy)
	log.Printf("UDP proxy %s listening on %d", proxy.Name, proxy.RemotePort)
	return nil
}

func acceptUDPPackets(conn *net.UDPConn, client *ClientInfo, proxy *ProxyInfo) {
	buf := make([]byte, 65535)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("UDP listener for proxy %s closed: %v", proxy.Name, err)
			return
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		go handleUDPPacket(conn, remoteAddr, data, client, proxy)
	}
}

func handleUDPPacket(conn *net.UDPConn, addr *net.UDPAddr, data []byte, client *ClientInfo, proxy *ProxyInfo) {
	stream, err := client.Session.OpenStream()
	if err != nil {
		log.Printf("Failed to open UDP stream: %v", err)
		return
	}
	defer stream.Close()

	streamID := stream.ID()
	nameBytes := []byte(proxy.Name)
	nameLen := len(nameBytes)
	msgBuf := make([]byte, 8+nameLen)

	// Message format: [MsgType][StreamID][RemotePort][NameLen][ProxyName]
	msgBuf[0] = tunnel.MsgTypeNewStream
	binary.BigEndian.PutUint32(msgBuf[1:5], streamID)
	binary.BigEndian.PutUint16(msgBuf[5:7], proxy.RemotePort)
	msgBuf[7] = byte(nameLen)
	if nameLen > 0 {
		copy(msgBuf[8:], nameBytes)
	}
	if _, err := stream.Write(msgBuf); err != nil {
		log.Printf("Failed to write NewStream: %v", err)
		return
	}

	// forward udp msg to client
	length := make([]byte, 2)
	binary.BigEndian.PutUint16(length, uint16(len(data)))
	if _, err := stream.Write(length); err != nil {
		return
	}
	if _, err := stream.Write(data); err != nil {
		return
	}

	// bidirectional forwarding loop (read from client and forward to external udp port)
	for {
		lenBuf := make([]byte, 2)
		if _, err := io.ReadFull(stream, lenBuf); err != nil {
			if err != io.EOF {
				log.Printf("UDP stream read error: %v", err)
			}
			return
		}
		l := binary.BigEndian.Uint16(lenBuf)
		buf := make([]byte, l)
		if _, err := io.ReadFull(stream, buf); err != nil {
			log.Printf("UDP stream read error: %v", err)
			return
		}
		if _, err := conn.WriteToUDP(buf, addr); err != nil {
			log.Printf("Failed to write UDP response: %v", err)
			return
		}
	}
}
