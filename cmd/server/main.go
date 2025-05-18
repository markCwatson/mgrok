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

	var listener net.Listener
	listener, err = net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	log.Printf("Server listening on :%d", *port)

	for {
		var conn net.Conn
		conn, err = listener.Accept()
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

	// Wait for session to be done (connection-level termination)
	<-session.CloseChan()
	log.Printf("Client %s disconnected", clientID)
}
