## TCP tunneling in mgrok

Below is a high-level walkthrough of how mgrok implements TCP tunneling. It is broken down into four stages:

1. Control connection & multiplexing
2. Proxy registration
3. TCP proxy data path

---

## 1. Control connection & multiplexing

Both the client and the server share a single long-lived TCP connection (on port 9000 by default) over which all control messages and data are transmitted. This connection is multiplexed using the `xtaci/smux` library, allowing multiple logical streams to flow over a single physical connection.

### Client side: establishing the session

The client establishes a TCP connection to the server and wraps it in a smux session:

```go
// cmd/client/main.go
conn, err = net.Dial("tcp", config.Server)
session, err = smux.Client(conn, nil)
```

The first stream opened is designated as the control stream, which will remain open for the entire session duration:

```go
ctrlStream, err = session.OpenStream()
proxyHandler.RegisterProxies(ctrlStream)
```

### Server side: accepting connections

The server listens for TCP connections and wraps accepted connections in smux sessions:

```go
// cmd/server/main.go
conn, err = ln.Accept()
session, err = smux.Server(conn, nil)
```

The first stream from each session is treated as the control stream:

```go
ctrlStream, err = session.AcceptStream()
go controlHandler.HandleConnection(ctrlStream, session, clientID)
```

---

## 2. Proxy registration handshake

The client registers each proxy defined in its configuration (`client.yaml`) by sending registration messages to the server over the control stream.

### Message format

```
<Handshake> : 4 bytes "GRT1" + uint8 authMethod + authPayload…
<Register>   : msgType=0x01 | uint8 proxyType | uint16 remotePort | uint16 localPort | N bytes name
```

### Client sends proxy registrations

```go
// internal/client/proxy/handler.go
func (h *Handler) RegisterProxies(stream *smux.Stream) {
    tunnel.WriteHandshake(stream, tunnel.AuthMethodToken, []byte(h.config.Token))

    for name, proxy := range h.config.Proxies {
        tunnel.WriteRegister(
            stream,
            proxyType,                    // TCP=1, UDP=2
            uint16(proxy.RemotePort),     // Port exposed on server
            uint16(proxy.LocalPort),      // Local service port
            name                          // Proxy identifier
        )

        h.activeProxies[name] = &Proxy{...}
    }
}
```

### Server handles registrations

The server processes each registration and starts the appropriate listeners:

```go
// internal/server/controller/handler.go
func (h *Handler) handleRegisterMsg(client *proxy.ClientInfo, data []byte) {
    proxyType := data[0]
    remotePort := binary.BigEndian.Uint16(data[1:3])
    localPort := binary.BigEndian.Uint16(data[3:5])
    name := string(data[5:])

    newProxy, err := h.proxyManager.RegisterProxy(client, name, proxyType, remotePort, localPort)

    if proxyType == tunnel.ProxyTypeTCP {
        proxy.StartTCPListener(newProxy, client)
    }
}
```

---

## 3. TCP proxy data path

When a user connects to an exposed port on the server, the server creates a new smux stream to the client and sends information about which proxy was requested (over port 9000 by default).

### Server side: incoming connection handling

```go
// internal/server/proxy/tcp.go
func acceptConnections(listener net.Listener, client *ClientInfo, proxy *ProxyInfo) {
    for {
        conn, err := listener.Accept()
        go handleProxyConnection(conn, client, proxy)
    }
}

func handleProxyConnection(conn net.Conn, client *ClientInfo, proxy *ProxyInfo) {
    // Open a new stream to the client
    stream, err := client.Session.OpenStream()

    // Send NewStream message with proxy information
    // Format: msgType + streamID + remotePort + nameLen + proxyName
    msgBuf[0] = tunnel.MsgTypeNewStream
    binary.BigEndian.PutUint32(msgBuf[1:5], streamID)
    binary.BigEndian.PutUint16(msgBuf[5:7], proxy.RemotePort)
    msgBuf[7] = byte(nameLen)
    copy(msgBuf[8:], nameBytes)

    // Copy data bidirectionally
    go io.Copy(stream, conn)  // user → client
    io.Copy(conn, stream)     // client → user
}
```

### Client side: handling stream requests

When the client receives a new stream, it determines which local service to connect to based on the proxy information:

```go
// internal/client/proxy/handler.go
func (h *Handler) handleNewStream(stream *smux.Stream) {
    // Parse stream ID and proxy info
    streamID := binary.BigEndian.Uint32(streamIDBuf)
    remotePort := binary.BigEndian.Uint16(headerBuf[0:2])
    nameLen := int(headerBuf[2])
    proxyName := string(nameBytes)

    // Find the matching local service
    // (first by name, then by remote port)

    // Connect to the local service
    localConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", localPort))

    // Copy data bidirectionally
    go io.Copy(stream, localConn)  // local → server
    io.Copy(localConn, stream)     // server → local
}
```

---

## Summary

- A single TCP connection is multiplexed to carry both control messages and data streams
- The control stream handles registration, heartbeats, and administrative messages
- For each user connection, a new multiplexed stream is created within the session
- TCP proxying works by copying data between user connection ↔ multiplexed stream ↔ local service
- The server includes proxy identification in each new stream request
- The client matches this information to connect to the correct local service

This approach allows many services to be exposed through a single connection, with minimal overhead and maximum efficiency.

### UDP support

UDP proxies follow the same registration flow but the server binds a `UDPConn`
instead of a TCP listener. Each datagram read from the public socket is sent to
the client over a new multiplexed stream with a 2 byte length prefix. The client
sends any responses back on the same stream. This keeps UDP traffic connection
less while still using the reliable TCP tunnel.

The length prefixing is necessary because the tunnel uses a TCP-like stream, but UDP is packet-based. The length prefix ensures packet boundaries are preserved when transmitting UDP data over the stream-based tunnel connection.
