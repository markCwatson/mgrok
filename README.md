# mgrok

A tunneling application for exposing local servers behind NATs and firewalls to the internet. In the demo below, the mgrok server (top-left) and client (top-right) are running concurrently. A local web server on port 8080 (bottom-left) hosts a simple HTML page; then, from the bottom-right tab I 'curl' the mgrok server's public port 8000. The request is forwarded over the tunnel to the client, which fetches the page from localhost:8080 and sends it back through the server to 'curl'.

![alt-text][8]

## Features planed

1. Basic TCP tunnel ✅
2. TCP tunnel with `smux` + multiple TCP proxies ✅
3. YAML config ✅
4. TLS support ✅
5. Simple Auth ✅

## Getting Started

### Prerequisites

#### Installing Go on macOS

1. **Install Go**:

   ```bash
   brew install go
   go version
   ```

   You should see something like `go version go1.24.3 darwin/arm64`

2. **Set up GOPATH** (add to your ~/.zshrc or ~/.bash_profile):
   ```bash
   export GOPATH=$HOME/go
   export PATH=$PATH:$GOPATH/bin
   ```

### Setup Project

1. **Clone this repository and install dependencies**:

   ```bash
   git clone https://github.com/markCwatson/mgrok.git
   cd mgrok
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

TLS must be setup and configured (the following instructions were tested on Mac).

1. **Install mkcert** - We'll use `mkcert` to handle the TLS certificate.

   ```
   brew install mkcert nss
   ```

2. **Install a local Certificate Authority** - Bootstrap a local CA and add it to your trust store.

   ```
   mkcert -install
   ```

3. **Generate a cert for localhost** -Generate the `.pem` files in `mgrok/certs/` then return to the root of this repo.

   ```
   cd certs
   mkcert localhost 127.0.0.1 ::1
   cd -
   ```

4. **Configure mgrk server** - Update the `configs/server.yaml` file with your files/paths.

   ```yaml
   enable_tls: true
   tls_cert_file: ~/repos/mgrok/certs/localhost+2.pem
   tls_key_file: ~/repos/mgrok/certs/localhost+2-key.pem
   bind_addr: 127.0.0.1
   bind_port: 9000
   auth_token: your-secret-token-here
   ```

5. **Start a local service** - For testing, run the web server (configured for https) in the `web/` directory.

   ```
   python web/server.py
   ```

6. **Configure your proxies** - The `configs/client.yaml` file defines which local services to expose. Configure it for the web proxy.

   ```yaml
   server: localhost:9000
   token: your-secret-token-here
   proxies:
     web:
       type: tcp
       local_port: 8080
       remote_port: 8000
   ```

7. **Start your server and client**:

   ```
   ./build/mgrok-server
   ./build/mgrok-client
   ```

8. **Verify proxy registration** - The client will register all proxies defined in the config. You should see

   ```
   Registered proxy web: tcp port 8080 -> 8000
   ```

9. **Test the tunnel** - Connect to the exposed port on your mgrok server using TLS.

   ```
   curl https://localhost:8000
   ```

You should see the text html page returned. This test shows:

1. A user connects to the exposed server port
2. Server creates a data stream to client
3. Client identifies which proxy was requested and connects to the corresponding local service
4. Data is copied bidirectionally through the multiplexed tunnel
5. TLS support

Note: you can disable TLS by setting `enable_tls: false` in `configs/server.yaml` (the client will fallback to TCP if the TLS handshake fails).

### Authentication

mgrok uses a simple token-based authentication to secure connections between the client and server:

1. **Token Configuration**:

   - Server: Set the `auth_token` field in `configs/server.yaml`
   - Client: Set the `token` field in `configs/client.yaml`
   - Both tokens must match exactly for authentication to succeed

2. **How it works**:

   - During handshake, the client sends its token to the server
   - The server validates this token against its configured auth_token
   - If they match, the connection is authenticated and allowed to proceed
   - If they don't match, the server rejects the connection

3. **Security**:
   - Keep your token values private and secure
   - Use a strong, unique token (UUID or random string)
   - The token is kept private and not logged in plaintext

Example tokens in config files:

```yaml
# Server (configs/server.yaml)
auth_token: 0196e9bd-dab3-7d51-a89c-4fcc68e3a811

# Client (configs/client.yaml)
token: 0196e9bd-dab3-7d51-a89c-4fcc68e3a811
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

[1]: https://github.com/xtaci/smux?utm_source=chatgpt.com 'GitHub - xtaci/smux: A Stream Multiplexing Library for golang with ...'
[2]: https://github.com/hashicorp/yamux?utm_source=chatgpt.com 'GitHub - hashicorp/yamux: Golang connection multiplexing library'
[3]: https://github.com/xtaci/kcp-go?utm_source=chatgpt.com 'A Crypto-Secure Reliable-UDP Library for golang with FEC'
[4]: https://pkg.go.dev/github.com/xtaci/smux?utm_source=chatgpt.com 'smux package - github.com/xtaci/smux - Go Packages'
[5]: https://pkg.go.dev/github.com/xtaci/kcp-go?utm_source=chatgpt.com 'kcp package - github.com/xtaci/kcp-go - Go Packages'
[6]: https://pkg.go.dev/crypto/tls?utm_source=chatgpt.com 'tls package - crypto/tls - Go Packages'
[7]: docs/tunneling.md 'tunneling in mgrok document'
[8]: assets/mgrok-demo-1.gif 'An mgrok demo'
