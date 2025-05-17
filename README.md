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
   git clone https://github.com/markCwatson/mgrok.git
   cd mgrok
   ```

2. **Install dependencies**:
   ```bash
   go mod tidy
   ```

Go stores all dependencies in a central cache, typically at:
`$GOPATH/pkg/mod/` (usually `~/go/pkg/mod/` on macOS).

### Building

You can build the project using the provided script:

```bash
./scripts/build.sh
```

This will create the following architecture-specific binaries in the `build` directory:

- `mgrok-server`: The server component
- `mgrok-client`: The client component

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

## Core architecture

1. **Public server**: Listens on a well‑known TCP port (e.g. :9000) for _control tunnels_ from clients. For every service the client wants to expose, it also opens a _public listener_ (TCP or UDP) on demand and forwards traffic through the tunnel. _Go primitives/libs_: `net.Listen`, `net.ListenPacket`; optional TLS (`crypto/tls`).

2. **Client (behind NAT)**: Reads a config file; dials the server with TLS; authenticates; registers one or more _proxies_ (`ssh`, `web`, `udp‑game`, …); keeps the control connection alive; for each incoming stream/packet from the server, opens/uses a local socket and pipes bytes both directions. _Go primitives/libs_: `net.Dial`, goroutines, `io.Copy`; YAML/INI parser.

3. **Multiplexing layer**: Allows many logical streams over one physical TCP/TLS connection so you don't need 1 × TCP socket per proxied connection. _Go primitives/libs_: `smux` ([GitHub][1]) or `yamux` ([GitHub][2]) (both production‑grade).

4. **Reliable‑UDP option** (future): If you want "UDP but reliable, congestion‑controlled" (like frp's `kcp` mode) you can swap the physical link with **kcp‑go**. _Go primitives/libs_: `kcp-go` ([GitHub][3]).

## Stages of TCP/UDP Tunneling

To read more, see [this doc on tunneling in mgrok][7]. Here is the summary from that doc:

- Control channel (TCP) carries JSON-framed control messages (NewProxy, StartWorkConn, UDPPacket, Ping, …) multiplexed via a yamux-style transporter.
- "NewProxy" handshake tells the server which proxy (TCP/UDP/etc.) to open and returns the remoteAddr to listen on.
- TCP proxy: the server listens on a TCP port and for each incoming connection grabs a workConn to the client; the client connects that workConn to the local service and shuttles bytes.
- UDP proxy: the server binds a UDP socket and sends/receives each datagram as a base64-encoded msg.UDPPacket over the workConn; on the client side the packet is unwrapped and forwarded to the local UDP service (and vice versa).

## Control protocol (minimal)

```<Handshake> : 4 bytes "GRT1" + uint8 authMethod + authPayload…
<Register>   : msgType=0x01 | uint8 proxyType | uint16 remotePort | uint16 localPort | N bytes name
<NewStream>  : msgType=0x02 | uint32 streamID
<Data>       : msgType=0x03 | uint32 streamID | uint16 length | …bytes…
<Close>      : msgType=0x04 | uint32 streamID
<Heartbeat>  : msgType=0x05
```

_Keep it binary and fixed‑length for speed; frame it with `smux`/`yamux` so you rarely have to re‑invent back‑pressure, windowing, etc._

## Security checklist

1. **Transport**: Wrap the initial TCP connection in TLS (`crypto/tls`) and disable < TLS 1.2. ([Go Packages][6])
2. **Authentication**: HMAC‑SHA256 token or mTLS; fail fast on mismatch.
3. **Authorisation**: Server config lists which client can open which remote ports/domains.
4. **Hardening**: Rate‑limit registrations; heartbeat every 15 s; close idle sessions; set `TCP_NODELAY` false to let Nagle help small frames.

## Configuration file (client example)

```yaml
server: tunnel.example.com:9000
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

## Packaging & DX

1. **GoReleaser** → multi‑arch binaries + `.deb`/`.rpm`.
2. Provide a single‑binary server and single‑binary client.
3. Systemd unit examples (`After=network.target; Restart=always`).
4. Optional: Docker images (`scratch` or `alpine`).

---

## Milestone plan

1. **Week 1**: Basic TCP tunnel (single proxy, no multiplex)
2. **Week 2**: Replace with `smux` + multiple TCP proxies
3. **Week 3**: Add UDP forwarding (stateless map)
4. **Week 4**: YAML config + reconnect logic + heartbeats
5. **Week 5**: TLS + token auth; graceful shutdown
6. **Week 6**: Packaging (GoReleaser) & docs; test behind real NAT

[1]: https://github.com/xtaci/smux?utm_source=chatgpt.com 'GitHub - xtaci/smux: A Stream Multiplexing Library for golang with ...'
[2]: https://github.com/hashicorp/yamux?utm_source=chatgpt.com 'GitHub - hashicorp/yamux: Golang connection multiplexing library'
[3]: https://github.com/xtaci/kcp-go?utm_source=chatgpt.com 'A Crypto-Secure Reliable-UDP Library for golang with FEC'
[4]: https://pkg.go.dev/github.com/xtaci/smux?utm_source=chatgpt.com 'smux package - github.com/xtaci/smux - Go Packages'
[5]: https://pkg.go.dev/github.com/xtaci/kcp-go?utm_source=chatgpt.com 'kcp package - github.com/xtaci/kcp-go - Go Packages'
[6]: https://pkg.go.dev/crypto/tls?utm_source=chatgpt.com 'tls package - crypto/tls - Go Packages'
[7]: docs/tunneling.md 'tunneling in mgrok document'
