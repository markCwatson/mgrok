package tunnel

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
)

const (
	// Protocol message types
	MsgTypeRegister  = 0x01
	MsgTypeNewStream = 0x02
	MsgTypeData      = 0x03
	MsgTypeClose     = 0x04
	MsgTypeHeartbeat = 0x05

	// Proxy types
	ProxyTypeTCP = 0x01
	ProxyTypeUDP = 0x02

	// Auth methods
	AuthMethodToken = 0x01
	AuthMethodmTLS  = 0x02
)

// Updated protocol message formats:
//
// <Handshake> : 4 bytes "GRT1" + uint8 authMethod + authPayload…
// <Register>   : msgType=0x01 | uint8 proxyType | uint16 remotePort | uint16 localPort | N bytes name
// <NewStream>  : msgType=0x02 | uint32 streamID | uint16 remotePort | uint8 nameLen | N bytes name
// <Data>       : msgType=0x03 | uint32 streamID | uint16 length | …bytes…
// <Close>      : msgType=0x04 | uint32 streamID
// <Heartbeat>  : msgType=0x05

// Protocol handshake: 4 bytes "GRT1" + uint8 authMethod + authPayload
type Handshake struct {
	Magic       [4]byte // "GRT1"
	AuthMethod  uint8
	AuthPayload []byte
}

// Register message: msgType=0x01 | uint8 proxyType | uint16 remotePort | uint16 localPort | N bytes name
type RegisterMsg struct {
	ProxyType  uint8
	RemotePort uint16
	LocalPort  uint16
	Name       string
}

// NewStream message: msgType=0x02 | uint32 streamID | uint16 remotePort | uint8 nameLen | N bytes name
type NewStreamMsg struct {
	StreamID   uint32
	RemotePort uint16
	NameLen    uint8
	Name       string
}

// Data message: msgType=0x03 | uint32 streamID | uint16 length | …bytes…
type DataMsg struct {
	StreamID uint32
	Length   uint16
	Data     []byte
}

// Close message: msgType=0x04 | uint32 streamID
type CloseMsg struct {
	StreamID uint32
}

// WriteHandshake writes a protocol handshake to any io.Writer (such as a control stream)
func WriteHandshake(w io.Writer, authMethod uint8, authPayload []byte) error {
	// Create the full handshake message
	handshake := append([]byte("GRT1"), authMethod)
	handshake = append(handshake, authPayload...)

	if len(authPayload) > 0 {
		log.Printf("Sending handshake: [% x] + auth payload (%d bytes)", append([]byte("GRT1"), authMethod), len(authPayload))
	} else {
		log.Printf("Sending handshake: [% x]", handshake)
	}

	// Write the full handshake in one call to ensure atomic write
	_, err := w.Write(handshake)
	if err != nil {
		return fmt.Errorf("failed to write handshake: %w", err)
	}

	return nil
}

// WriteRegister writes a register message to any io.Writer (such as a control stream)
func WriteRegister(w io.Writer, proxyType uint8, remotePort, localPort uint16, name string) error {
	msgBuf := make([]byte, 0, 10+len(name))

	msgBuf = append(msgBuf, MsgTypeRegister)
	msgBuf = append(msgBuf, proxyType)
	portBuf := make([]byte, 4)
	binary.BigEndian.PutUint16(portBuf, remotePort)
	binary.BigEndian.PutUint16(portBuf[2:], localPort)
	msgBuf = append(msgBuf, portBuf...)
	msgBuf = append(msgBuf, []byte(name)...)

	log.Printf("Sending register message (%d bytes): [% x]", len(msgBuf), msgBuf)
	log.Printf("Register details: type=%d, remote=%d, local=%d, name=%s",
		proxyType, remotePort, localPort, name)

	// Write the full message in one call
	_, err := w.Write(msgBuf)
	if err != nil {
		return fmt.Errorf("failed to write register message: %w", err)
	}

	return nil
}
