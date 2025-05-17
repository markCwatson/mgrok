## TCP/UDP tunneling in mgrok

Below is a high-level "code-walkthrough" of how mgrok implements both TCP and UDP tunneling. It is broken down into four stages:

1. Control connection & messaging
2. Proxy registration handshake (NewProxy / NewProxyResp)
3. TCP proxy data path
4. UDP proxy data path

---

## 1. Control connection & messaging

Both the client and the server share a single long-lived "control" connection over which JSON-encoded control messages are exchanged (NewProxy, StartWorkConn, UDPPacket, Ping, etc.). On top of that wire they multiplex messages concurrently.

### Client side: setting up the dispatcher & transporter

In the client's Control object, the client wraps the raw net.Conn (or encrypted wrapper) in a msg.Dispatcher and then builds a transport.MessageTransporter over it:

```go
// client/control.go
...
ctl.msgDispatcher = msg.NewDispatcher(sessionCtx.Conn) // or cryptoRW
ctl.registerMsgHandlers()
ctl.msgTransporter = transport.NewMessageTransporter( // <– multiplexing layer
    ctl.msgDispatcher.SendChannel(),
)
...
```

The msgDispatcher demultiplexes inbound JSON messages by type, and the MessageTransporter lets downstream code send control messages concurrently over the same connection.

### Server side: registering handlers

On the server the control loop registers handlers for the various messages (NewProxy, ReqWorkConn, Ping, etc.):

```go
// server/control.go
func (ctl *Control) registerMsgHandlers() {
    ctl.msgDispatcher.RegisterHandler(&msg.NewProxy{}, ctl.handleNewProxy)
    ctl.msgDispatcher.RegisterHandler(&msg.Ping{}, ctl.handlePing)
    ctl.msgDispatcher.RegisterHandler(&msg.ReqWorkConn{}, msg.AsyncHandler(ctl.handleReqWorkConn))
    ctl.msgDispatcher.RegisterHandler(&msg.NewProxyResp{}, ctl.handleNewProxyResp)
    ...
}
```

---

## 2. Proxy registration handshake

Before any traffic is tunneled, the client tells the server "please open me a proxy" via a msg.NewProxy request; the server replies with msg.NewProxyResp containing either an error or the RemoteAddr to listen on.

### Client sends NewProxy

The client's proxy-manager periodically checks each Proxy's state and, when it needs to start, emits a NewProxy payload via the event/transport layer:

```go
// client/proxy/proxy_wrapper.go
var newProxyMsg msg.NewProxy
pw.Cfg.MarshalToMsg(&newProxyMsg) // map the CLI/config into JSON fields
_ = pw.handler(&event.StartProxyPayload{ // handler = MessageTransporter.Send
    NewProxyMsg: &newProxyMsg,
})
```

Then the transporter sends it in-band:

```go
// client/proxy/proxy_manager.go
func (pm *Manager) HandleEvent(payload any) error {
    var m msg.Message
    switch e := payload.(type) {
    case *event.StartProxyPayload:
        m = e.NewProxyMsg
        ...
    }
    return pm.msgTransporter.Send(m)
}
```

### Server handles NewProxy

the server receives msg.NewProxy, validates it, instantiates the appropriate Proxy object, calls its Run(), and finally replies with NewProxyResp:

```go
// server/control.go
func (ctl *Control) handleNewProxy(m msg.Message) {
    inMsg := m.(*msg.NewProxy)

    // plugin hook omitted...

    // actually register the proxy: alloc ports, start listeners, etc.
    if remoteAddr, err := ctl.RegisterProxy(inMsg); err != nil {
        resp.Error = fmt.Sprintf("new proxy error: %v", err)
    } else {
        resp.RemoteAddr = remoteAddr
    }
    _ = ctl.msgDispatcher.Send(resp)  // send NewProxyResp
}
```

The core of RegisterProxy does the work of creating and running the server-side proxy:

```go
// server/control.go
func (ctl *Control) RegisterProxy(pxyMsg *msg.NewProxy) (remoteAddr string, err error) {
    // build the v1.ProxyConfigurer from the JSON message
    pxyConf, err := config.NewProxyConfigurerFromMsg(pxyMsg, ctl.serverCfg)
    ...
    // NewProxy returns a Proxy interface (TCPProxy, UDPProxy, HTTPProxy, etc.)
    pxy, err := proxy.NewProxy(..., pxyConf, ctl.serverCfg)
    ...
    remoteAddr, err = pxy.Run() // open listeners
    ...
    ctl.pxyManager.Add(pxyMsg.ProxyName, pxy)
    return
}
```

### Client handles NewProxyResp

Back in the client, Control.handleNewProxyResp marks the proxy as running or errored:

```go
// client/control.go
func (ctl *Control) handleNewProxyResp(m msg.Message) {
    inMsg := m.(*msg.NewProxyResp)
    err := ctl.pm.StartProxy(inMsg.ProxyName, inMsg.RemoteAddr, inMsg.Error)
    ...
}
```

---

## 3. TCP proxy data path

Once the proxy handshake is done, the server begins listening on the target TCP port, and for each incoming user connection it grabs a "work connection" to the client and tunnels bytes back and forth. The client accepts that work connection and forwards the data to/from the local service.

### 3.1 Server-side TCP listener

When you start a TCP proxy, the server does roughly:

```go
// server/proxy/tcp.go
func (pxy *TCPProxy) Run() (remoteAddr string, err error) {
    // acquire or bind a listening port on server side
    listener, errRet := net.Listen("tcp", net.JoinHostPort(pxy.serverCfg.ProxyBindAddr, strconv.Itoa(pxy.deploymentPort)))
    pxy.listeners = append(pxy.listeners, listener)
    xlog.FromContextSafe(pxy.ctx).Infof("tcp proxy listen port [%d]", pxy.cfg.RemotePort)

    pxy.cfg.RemotePort = pxy.realBindPort
    remoteAddr = fmt.Sprintf(":%d", pxy.realBindPort)
    pxy.startCommonTCPListenersHandler()   // spawn accept loop
    return
}
```

The shared accept/dispatch code in BaseProxy then spins off a goroutine per listener:

```go
// server/proxy/proxy.go
func (pxy *BaseProxy) startCommonTCPListenersHandler() {
    for _, listener := range pxy.listeners {
        go func(l net.Listener) {
            for {
                c, err := l.Accept()
                if err != nil {
                    xlog.FromContextSafe(pxy.ctx).Warnf("listener closed: %v", err)
                    return
                }
                xlog.FromContextSafe(pxy.ctx).Infof("get a user connection [%s]", c.RemoteAddr())
                go pxy.handleUserTCPConnection(c)
            }
        }(listener)
    }
}
```

### 3.2 Server-side binding user↔workConn

Each accepted userConn is paired with a pooled "work connection" to the client. Bytes flow from userConn↔workConn:

```go
// server/proxy/proxy.go
func (pxy *BaseProxy) handleUserTCPConnection(userConn net.Conn) {
    defer userConn.Close()

    // pull a ready workConn (client→server TCP stream)
    workConn, err := pxy.GetWorkConnFromPool(userConn.RemoteAddr(), userConn.LocalAddr())
    if err != nil {
        return
    }
    defer workConn.Close()

    // apply optional encryption/compression/limiter on workConn side
    local := workConn
    if pxy.GetLimiter() != nil { ... }
    if pxy.configurer.GetBaseConfig().Transport.UseEncryption { ... }
    if pxy.configurer.GetBaseConfig().Transport.UseCompression { ... }

    // finally join the two streams:
    inCount, outCount, _ := libio.Join(local, userConn)
    metrics.Server.AddTrafficIn( ... , inCount)
    metrics.Server.AddTrafficOut(... , outCount)
}
```

### 3.3 Client-side receiving workConn & piping to local service

On the client side, BaseProxy.InWorkConn is called when a StartWorkConn message arrives; that in turn calls HandleTCPWorkConnection:

```go
// client/proxy/proxy.go
func (pxy *BaseProxy) InWorkConn(conn net.Conn, m *msg.StartWorkConn) {
    if pxy.inWorkConnCallback != nil && !pxy.inWorkConnCallback(...) {
        return
    }
    pxy.HandleTCPWorkConnection(conn, m, []byte(pxy.clientCfg.Auth.Token))
}

// common handler for tcp work connections
func (pxy *BaseProxy) HandleTCPWorkConnection(workConn net.Conn, m *msg.StartWorkConn, encKey []byte) {
    // apply limiter/encryption/compression
    remote := workConn
    if pxy.limiter != nil { ... }
    if pxy.baseCfg.Transport.UseEncryption { ... }
    if pxy.baseCfg.Transport.UseCompression { ... }

    // dial the local service
    localConn, err := libnet.Dial(net.JoinHostPort(baseCfg.LocalIP, strconv.Itoa(baseCfg.LocalPort)), ...)
    if err != nil { workConn.Close(); return }

    // shuttle data between workConn and localConn
    _, _, errs := libio.Join(localConn, remote)
    ...
}
```

The "general TCP" proxy factory wires up all TCP-based protocols (raw TCP, HTTP, HTTPS, STCP, TCPMux):

```go
// client/proxy/general_tcp.go
func init() {
    pxyConfs := []v1.ProxyConfigurer{
        &v1.TCPProxyConfig{},
        &v1.HTTPProxyConfig{},
        &v1.HTTPSProxyConfig{},
        &v1.STCPProxyConfig{},
        &v1.TCPMuxProxyConfig{},
    }
    for _, cfg := range pxyConfs {
        RegisterProxyFactory(reflect.TypeOf(cfg), NewGeneralTCPProxy)
    }
}

type GeneralTCPProxy struct{ *BaseProxy }
func NewGeneralTCPProxy(baseProxy *BaseProxy, _ v1.ProxyConfigurer) Proxy {
    return &GeneralTCPProxy{BaseProxy: baseProxy}
}
```

---

## 4. UDP proxy data path

UDP tunneling in mgrok must multiplex unreliable datagrams over TCP. mgrok does this by framing each UDP packet in a msg.UDPPacket and sending it over the workConn, plus heartbeats (msg.Ping) to detect closure.

### 4.1 UDP packet framing helpers

The shared helper in pkg/proto/udp encodes each datagram as base64 in a JSON message and offers in-process forwarding functions:

```go
// pkg/proto/udp/udp.go
func NewUDPPacket(buf []byte, laddr, raddr *net.UDPAddr) *msg.UDPPacket {
    return &msg.UDPPacket{
        Content: base64.StdEncoding.EncodeToString(buf),
        LocalAddr: laddr,
        RemoteAddr: raddr,
    }
}

func GetContent(m *msg.UDPPacket) (buf []byte, err error) {
    return base64.StdEncoding.DecodeString(m.Content)
}
```

#### ForwardUserConn (server side)

Reads from the readCh (packets from client) and writes them to the UDP socket bound for external clients; then reads from that socket and pushes packets into sendCh back to the client:

```go
// pkg/proto/udp/udp.go
func ForwardUserConn(udpConn *net.UDPConn, readCh <-chan *msg.UDPPacket, sendCh chan<- *msg.UDPPacket, bufSize int) {
    // from client → external clients
    go func() {
        for udpMsg := range readCh {
            buf, err := GetContent(udpMsg)
            if err != nil { continue }
            _, _ = udpConn.WriteToUDP(buf, udpMsg.RemoteAddr)
        }
    }()
    // from external clients → client
    buf := pool.GetBuf(bufSize); defer pool.PutBuf(buf)
    for {
        n, remoteAddr, err := udpConn.ReadFromUDP(buf)
        if err != nil { return }
        udpMsg := NewUDPPacket(buf[:n], nil, remoteAddr)
        select { case sendCh <- udpMsg: default: }
    }
}
```

#### Forwarder (client side)

Binds a local UDP socket to your local service, and bridges packets between readCh (from server) and sendCh (back to server) with a per-remote-client net.UDPConn:

```go
// pkg/proto/udp/udp.go
func Forwarder(dstAddr *net.UDPAddr, readCh <-chan *msg.UDPPacket, sendCh chan<- msg.Message, bufSize int) {
    var mu sync.RWMutex
    udpConnMap := make(map[string]*net.UDPConn)

    // writerFn reads replies from local service → sendCh
    ...
    // pump packets from readCh → local service dstAddr
    go func() {
        for udpMsg := range readCh {
            buf, _ := GetContent(udpMsg)
            mu.Lock()
            udpConn, ok := udpConnMap[udpMsg.RemoteAddr.String()]
            if !ok {
                udpConn, _ = net.DialUDP("udp", nil, dstAddr)
                udpConnMap[udpMsg.RemoteAddr.String()] = udpConn
            }
            mu.Unlock()
            udpConn.Write(buf)
            if !ok {
                go writerFn(udpMsg.RemoteAddr, udpConn)
            }
        }
    }()
}
```

### 4.2 Server-side UDP proxy

The server binds the public UDP port, then waits for work connections from the client. It spins up two goroutines on each workConn: one to read framed packets (UDPPacket) or heartbeat (Ping), the other to send packets back.

```go
// server/proxy/udp.go
func (pxy *UDPProxy) Run() (remoteAddr string, err error) {
    // bind the UDP port
    pxy.realBindPort, err = pxy.rc.UDPPortManager.Acquire(...)
    addr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", pxy.serverCfg.ProxyBindAddr, pxy.realBindPort))
    pxy.udpConn, _ = net.ListenUDP("udp", addr)
    pxy.sendCh = make(chan *msg.UDPPacket, 1024)
    pxy.readCh = make(chan *msg.UDPPacket, 1024)
    pxy.checkCloseCh = make(chan int)

    // spawn the read/write loops over the workConn pool
    go func() {
        time.Sleep(500 * time.Millisecond)
        for {
            workConn, err := pxy.GetWorkConnFromPool(nil, nil)
            if err != nil {
                time.Sleep(time.Second); continue
            }
            pxy.workConn.Close()  // drop old conn
            // wrap encryption/compression/limiter...
            pxy.workConn = wrappedConn
            go workConnReaderFn(pxy.workConn)
            go workConnSenderFn(pxy.workConn, ctx)
            <-pxy.checkCloseCh
        }
    }()

    // bind UDP port <> message channels
    go func() {
        udp.ForwardUserConn(pxy.udpConn, pxy.readCh, pxy.sendCh, int(pxy.serverCfg.UDPPacketSize))
        pxy.Close()
    }()
    return fmt.Sprintf(":%d", pxy.realBindPort), nil
}
```

### 4.3 Client-side UDP proxy

On the client, the Run() simply resolves the local UDP address; the real work begins when a workConn arrives (InWorkConn):

```go
// client/proxy/udp.go
func (pxy *UDPProxy) Run() error {
    pxy.localAddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", pxy.cfg.LocalIP, pxy.cfg.LocalPort))
    return err
}

func (pxy *UDPProxy) InWorkConn(conn net.Conn, _ *msg.StartWorkConn) {
    // drop prior state
    pxy.Close()

    // wrap encryption/compression/limiter...
    pxy.workConn = conn; pxy.readCh = make(chan *msg.UDPPacket, 1024); pxy.sendCh = make(chan msg.Message, 1024)

    // reader: workConn → readCh
    go workConnReaderFn(pxy.workConn, pxy.readCh)
    // writer: sendCh → workConn
    go workConnSenderFn(pxy.workConn, pxy.sendCh)
    // heartbeat ping
    go heartbeatFn(pxy.sendCh)

    // finally bridge localAddr ↔ readCh/sendCh
    udp.Forwarder(pxy.localAddr, pxy.readCh, pxy.sendCh, int(pxy.clientCfg.UDPPacketSize))
}
```

---

## Summary

- Control channel (TCP) carries JSON-framed control messages (NewProxy, StartWorkConn, UDPPacket, Ping, …) multiplexed via a yamux-style transporter.
- "NewProxy" handshake tells the server which proxy (TCP/UDP/etc.) to open and returns the remoteAddr to listen on.
- TCP proxy: the server listens on a TCP port and for each incoming connection grabs a workConn to the client; the client connects that workConn to the local service and shuttles bytes.
- UDP proxy: the server binds a UDP socket and sends/receives each datagram as a base64-encoded msg.UDPPacket over the workConn; on the client side the packet is unwrapped and forwarded to the local UDP service (and vice versa).
