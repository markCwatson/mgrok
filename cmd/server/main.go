package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	"github.com/xtaci/smux"
)

func main() {
	var err error

	var port *int = flag.Int("port", 9000, "Port to listen on")
	flag.Parse()

	// Use plain TCP for development
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

		// Wrap in smux
		var session *smux.Session
		session, err = smux.Server(conn, nil)
		if err != nil {
			log.Printf("Failed to create smux session: %v", err)
			conn.Close()
			continue
		}

		// Handle client in a goroutine
		go serveClient(session)
	}
}

func serveClient(session *smux.Session) {
	defer session.Close()
	var err error

	// Accept the control stream
	var ctrlStream *smux.Stream
	ctrlStream, err = session.AcceptStream()
	if err != nil {
		log.Printf("Failed to accept control stream: %v", err)
		return
	}
	defer ctrlStream.Close()

	// Handle control messages
	go handleControl(ctrlStream, session)

	// Accept and handle other streams
	for {
		var stream *smux.Stream
		stream, err = session.AcceptStream()
		if err != nil {
			log.Printf("Error accepting stream: %v", err)
			return
		}

		go handleDataStream(stream)
	}
}

func handleControl(conn net.Conn, _ *smux.Session) {
	// TODO: Implement control protocol
	log.Println("Control connection established")

	// Just keep the connection alive for now
	buffer := make([]byte, 1024)
	for {
		_, err := conn.Read(buffer)
		if err != nil {
			log.Printf("Control connection closed: %v", err)
			break
		}
		// Process control messages here
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
