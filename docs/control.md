# Control Protocol

## Overview

The control protocol is the communication mechanism between mgrok client and server that enables tunneling. It operates over a dedicated multiplexed stream (the "control connection") and allows:

1. **Proxy Registration**: Clients register local services they want to expose
2. **Connection Management**: Server requests work connections when users connect to exposed services
3. **Heartbeat**: Both sides confirm the tunnel is still active
4. **Metadata Exchange**: Authentication and configuration information

## Terminology

- **Proxy**: A service exposure configuration that maps a local port (client-side) to a remote port (server-side)
- **Work Connection**: A multiplexed stream used to tunnel actual TCP/UDP traffic
- **Control Connection**: The dedicated stream used to exchange control messages

## Stream Types

The tunnel uses two main types of streams over a single multiplexed connection:

1. **Control Stream**: A dedicated stream that handles registration, heartbeats, and administrative messages. This stream is opened first and stays open for the duration of the session.

2. **Data Streams**: On-demand streams created to handle actual traffic forwarding. Each proxied connection gets its own data stream, allowing multiple concurrent connections to share the same TCP connection.

## Message Format

The protocol uses binary messages with the following format:

```
<Handshake> : 4 bytes "GRT1" + uint8 authMethod + authPayload…
<Register>   : msgType=0x01 | uint8 proxyType | uint16 remotePort | uint16 localPort | N bytes name
<NewStream>  : msgType=0x02 | uint32 streamID
<Data>       : msgType=0x03 | uint32 streamID | uint16 length | …bytes…
<Close>      : msgType=0x04 | uint32 streamID
<Heartbeat>  : msgType=0x05
```

## Proxy Types

The protocol supports two proxy types:

- TCP (0x01): Standard TCP connection forwarding
- UDP (0x02): Datagram forwarding via encapsulation

## Flow

1. Client connects to server and establishes a session
2. Client opens a control stream and registers desired proxies
3. Server binds requested ports and responds with success/failure
4. When users connect to server ports, server signals client via control protocol
5. Client establishes work connections to handle the actual data transfer
