# mgrok

A secure tunnel application for exposing local servers behind NATs and firewalls to the internet.

## Getting Started

### Prerequisites

#### Installing Go on macOS

1. **Install Homebrew** (if not already installed):

   ```bash
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
   ```

2. **Install Go**:

   ```bash
   brew install go
   ```

3. **Verify installation**:

   ```bash
   go version
   ```

   You should see something like `go version go1.24.3 darwin/arm64`

4. **Set up GOPATH** (add to your ~/.zshrc or ~/.bash_profile):
   ```bash
   export GOPATH=$HOME/go
   export PATH=$PATH:$GOPATH/bin
   ```

### Setup Project

1. **Clone this repository**:

   ```bash
   git clone https://github.com/yourusername/mgrok.git
   cd mgrok
   ```

2. **Install dependencies**:
   ```bash
   go mod tidy
   ```

### Building

You can build the project using the provided script:

```bash
./scripts/build.sh
```

This will create the following binaries in the `build` directory:

- `mgrok-server`: The server component
- `mgrok-client`: The client component

Alternatively, you can build each component manually:

```bash
# Build server
go build -o build/mgrok-server ./cmd/server

# Build client
go build -o build/mgrok-client ./cmd/client
```

### Running

#### Development Setup (Quick Start)

For local development and testing, TLS is disabled by default to simplify setup:

1. **Start the server** (in one terminal):

   ```bash
   ./build/mgrok-server
   ```

2. **Start the client** (in another terminal):
   ```bash
   ./build/mgrok-client --server localhost:9000
   ```

The client will automatically connect to the server running on your local machine. This allows you to quickly test the functionality without dealing with TLS certificates or domain name resolution.

#### Production Setup

For production use, you should enable TLS and proper authentication:

1. **Configure the server** by editing `configs/server.yaml`
2. **Generate TLS certificates** and place them in the `certs/` directory
3. **Run the server**:

   ```bash
   build/mgrok-server
   ```

4. **Configure the client** by editing `configs/client.yaml` with your server's domain
5. **Run the client**:
   ```bash
   build/mgrok-client
   ```

## Project Structure

```
mgrok/
├── cmd/
│   ├── client/          # Client application
│   └── server/          # Server application
├── configs/             # Configuration files
├── internal/            # Private packages
│   ├── auth/            # Authentication logic
│   ├── config/          # Configuration parsing
│   ├── multiplexer/     # Connection multiplexing
│   └── tunnel/          # Tunnel implementation
├── scripts/             # Build and utility scripts
└── docs/                # Documentation
```

## 1. Core architecture

1. **Public server**: Listens on a well‑known TCP port (e.g. :7000) for _control tunnels_ from clients. For every service the client wants to expose, it also opens a _public listener_ (TCP or UDP) on demand and forwards traffic through the tunnel. _Go primitives/libs_: `net.Listen`, `net.ListenPacket`; optional TLS (`crypto/tls`).

2. **Client (behind NAT)**: Reads a config file; dials the server with TLS; authenticates; registers one or more _proxies_ (`ssh`, `web`, `udp‑game`, …); keeps the control connection alive; for each incoming stream/packet from the server, opens/uses a local socket and pipes bytes both directions. _Go primitives/libs_: `net.Dial`, goroutines, `io.Copy`; YAML/INI parser.

3. **Multiplexing layer**: Allows many logical streams over one physical TCP/TLS connection so you don't need 1 × TCP socket per proxied connection. _Go primitives/libs_: `smux` ([GitHub][1]) or `yamux` ([GitHub][2]) (both production‑grade).

4. **Reliable‑UDP option** (future): If you want "UDP but reliable, congestion‑controlled" (like frp's `kcp` mode) you can swap the physical link with **kcp‑go**. _Go primitives/libs_: `kcp-go` ([GitHub][3]).

---

## 2. Minimal control protocol (suggestion)

```
<Handshake>  : 4 bytes "GRT1" + uint8 authMethod + authPayload…
<Register>   : msgType=0x01 | uint8 proxyType | uint16 remotePort | uint16 localPort | N bytes name
<NewStream>  : msgType=0x02 | uint32 streamID
<Data>       : msgType=0x03 | uint32 streamID | uint16 length | …bytes…
<Close>      : msgType=0x04 | uint32 streamID
<Heartbeat>  : msgType=0x05
```

_Keep it binary and fixed‑length for speed; frame it with `smux`/`yamux` so you rarely have to re‑invent back‑pressure, windowing, etc._

---

## 3. Skeleton flow (Go 1.22+)

```go
// server/main.go (abridged)
ln, _ := tls.Listen("tcp", ":7000", tlsCfg())
for {
    ctl, _ := ln.Accept()                 // 1. accept client
    s, _ := smux.Server(ctl, nil)         // 2. wrap in smux
    go serveClient(s)                     // 3. handle proxies
}

func serveClient(sess *smux.Session) {
    ctrlStr, _ := sess.AcceptStream()     // first stream = control
    go handleControl(ctrlStr, sess)
}

func handleControl(c net.Conn, sess *smux.Session) {
    for {
        msg := readMsg(c)
        switch msg.Type {
        case REG_PROXY:
            go startPublicListener(msg, sess) // TCP or UDP branch
        }
    }
}
```

```go
// client/main.go (abridged)
ctl, _ := tls.Dial("tcp", "server:7000", tlsCfg())
sess, _ := smux.Client(ctl, nil)
ctrlStr, _ := sess.OpenStream()          // dedicated control stream
sendRegisters(ctrlStr, config)

// Goroutine: dispatch every new smux stream.
go func() {
    for {
        s, _ := sess.AcceptStream()      // server ↔ client data stream
        go pipe(s, localDial(s.ID()))    // io.Copy both ways
    }
}()
```

_In ±300 lines you have a working TCP tunnel; UDP needs one extra mapping table._

---

## 4. Handling **UDP**

1. **Server side**

   ```go
   pc, _ := net.ListenPacket("udp", ":6000")
   buf := make([]byte, 1500)
   for {
       n, rAddr, _ := pc.ReadFrom(buf)
       stream := getStreamFor(rAddr) // map[ip:port]*smux.Stream
       stream.Write(frame(buf[:n]))
   }
   ```

2. **Client side**
   Same pattern but reversed: read from the stream, write to local `net.PacketConn`.

3. **State table**

   - Key: remote IP\:port
   - Value: `*smux.Stream` (or a small struct with timeout)
   - Clean up with a timer (e.g., 90 s of no packets).

4. **Advanced**: if you later want _reliable_ UDP for the tunnel itself (to avoid TCP‑over‑TCP), just replace the underlying dialer with **kcp-go**—`smux` works fine on top of it. ([Go Packages][4], [Go Packages][5])

---

## 5. Security checklist

1. **Transport**: Wrap the initial TCP connection in TLS (`crypto/tls`) and disable < TLS 1.2. ([Go Packages][6])
2. **Authentication**: HMAC‑SHA256 token or mTLS; fail fast on mismatch.
3. **Authorisation**: Server config lists which client can open which remote ports/domains.
4. **Hardening**: Rate‑limit registrations; heartbeat every 15 s; close idle sessions; set `TCP_NODELAY` false to let Nagle help small frames.

---

## 6. Configuration file (client example)

```yaml
server: tunnel.example.com:7000
token: 92c7…eab
proxies:
  ssh:
    type: tcp
    local_port: 22
    remote_port: 6000
  game:
    type: udp
    local_port: 7777
    remote_port: 7777
```

Parse with `gopkg.in/yaml.v3` and generate the register messages at startup.

---

## 7. Packaging & DX

1. **GoReleaser** → multi‑arch binaries + `.deb`/`.rpm`.
2. Provide a single‑binary server and single‑binary client.
3. Systemd unit examples (`After=network.target; Restart=always`).
4. Optional: Docker images (`scratch` or `alpine`).

---

## 8. Milestone plan

1. **Week 1**: Basic TCP tunnel (single proxy, no multiplex)
2. **Week 2**: Replace with `smux` + multiple TCP proxies
3. **Week 3**: Add UDP forwarding (stateless map)
4. **Week 4**: YAML config + reconnect logic + heartbeats
5. **Week 5**: TLS + token auth; graceful shutdown
6. **Week 6**: Packaging (GoReleaser) & docs; test behind real NAT

---

### References

[1]: https://github.com/xtaci/smux?utm_source=chatgpt.com 'GitHub - xtaci/smux: A Stream Multiplexing Library for golang with ...'
[2]: https://github.com/hashicorp/yamux?utm_source=chatgpt.com 'GitHub - hashicorp/yamux: Golang connection multiplexing library'
[3]: https://github.com/xtaci/kcp-go?utm_source=chatgpt.com 'A Crypto-Secure Reliable-UDP Library for golang with FEC'
[4]: https://pkg.go.dev/github.com/xtaci/smux?utm_source=chatgpt.com 'smux package - github.com/xtaci/smux - Go Packages'
[5]: https://pkg.go.dev/github.com/xtaci/kcp-go?utm_source=chatgpt.com 'kcp package - github.com/xtaci/kcp-go - Go Packages'
[6]: https://pkg.go.dev/crypto/tls?utm_source=chatgpt.com 'tls package - crypto/tls - Go Packages'
