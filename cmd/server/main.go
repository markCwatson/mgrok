package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	"github.com/markCwatson/mgrok/internal/server/controller"
	"github.com/markCwatson/mgrok/internal/server/proxy"
	"github.com/xtaci/smux"
)

var proxyManager *proxy.Manager
var controlHandler *controller.Handler

func init() {
	proxyManager = proxy.NewManager()
	controlHandler = controller.NewHandler(proxyManager)
}

func main() {
	var err error

	var port *int = flag.Int("port", 9000, "Port to listen on")
	flag.Parse()

	var ln net.Listener
	ln, err = net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()

	log.Printf("Server listening on :%d", *port)

	for {
		var conn net.Conn
		conn, err = ln.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		log.Printf("New connection from %s", conn.RemoteAddr())

		var session *smux.Session
		session, err = smux.Server(conn, nil)
		if err != nil {
			log.Printf("Failed to create smux session: %v", err)
			conn.Close()
			continue
		}

		go serveClient(session)
	}
}

func serveClient(session *smux.Session) {
	defer session.Close()
	var err error

	clientID := fmt.Sprintf("%p", session)
	proxyManager.AddClient(clientID, session)
	defer proxyManager.RemoveClient(clientID)

	// manages reg/heartbeat and stays open for the duration of the session
	var ctrlStream *smux.Stream
	ctrlStream, err = session.AcceptStream()
	if err != nil {
		log.Printf("Failed to accept control stream: %v", err)
		return
	}
	defer ctrlStream.Close()

	go controlHandler.HandleConnection(ctrlStream, session, clientID)

	for {
		// handles traffic (one per proxy)
		var dataStream *smux.Stream
		dataStream, err = session.AcceptStream()
		if err != nil {
			log.Printf("Error accepting data stream: %v", err)
			return
		}

		go handleDataStream(dataStream)
	}
}

func handleDataStream(stream *smux.Stream) {
	defer stream.Close()
	var err error

	log.Printf("New data stream established: %d", stream.ID())
	var buffer []byte = make([]byte, 1024)

	for {
		var n int
		n, err = stream.Read(buffer)
		if err != nil {
			log.Printf("Stream %d closed: %v", stream.ID(), err)
			return
		}

		var message string = string(buffer[:n])
		log.Printf("Received on stream %d: %s", stream.ID(), message)

		// Echo back with a prefix
		var response string = fmt.Sprintf("Server received: %s", message)
		_, err = stream.Write([]byte(response))
		if err != nil {
			log.Printf("Failed to write to stream %d: %v", stream.ID(), err)
			return
		}
	}
}
