package tunnel

import (
	"encoding/binary"
	"io"
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

// Protocol handshake: 4 bytes "GRT1" + uint8 authMethod + authPayload
type Handshake struct {
	Magic      [4]byte // "GRT1"
	AuthMethod uint8
	AuthPayload []byte
}

// Register message: msgType=0x01 | uint8 proxyType | uint16 remotePort | uint16 localPort | N bytes name
type RegisterMsg struct {
	ProxyType  uint8
	RemotePort uint16
	LocalPort  uint16
	Name       string
}

// NewStream message: msgType=0x02 | uint32 streamID
type NewStreamMsg struct {
	StreamID uint32
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

// WriteHandshake writes a protocol handshake to the writer
func WriteHandshake(w io.Writer, authMethod uint8, authPayload []byte) error {
	// Write magic
	if _, err := w.Write([]byte("GRT1")); err != nil {
		return err
	}
	
	// Write auth method
	if _, err := w.Write([]byte{authMethod}); err != nil {
		return err
	}
	
	// Write auth payload
	_, err := w.Write(authPayload)
	return err
}

// WriteRegister writes a register message to the writer
func WriteRegister(w io.Writer, proxyType uint8, remotePort, localPort uint16, name string) error {
	// Message type
	if _, err := w.Write([]byte{MsgTypeRegister}); err != nil {
		return err
	}
	
	// Proxy type
	if _, err := w.Write([]byte{proxyType}); err != nil {
		return err
	}
	
	// Ports
	portBuf := make([]byte, 4)
	binary.BigEndian.PutUint16(portBuf, remotePort)
	binary.BigEndian.PutUint16(portBuf[2:], localPort)
	if _, err := w.Write(portBuf); err != nil {
		return err
	}
	
	// Name
	_, err := w.Write([]byte(name))
	return err
}

// More message encoding/decoding functions would be implemented here 