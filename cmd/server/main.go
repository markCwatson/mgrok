package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/markCwatson/mgrok/internal/config"
	"github.com/markCwatson/mgrok/internal/server/controller"
	"github.com/markCwatson/mgrok/internal/server/proxy"
	"github.com/markCwatson/mgrok/internal/server/tls"
	"github.com/xtaci/smux"
)

var tlsManager *tls.Manager
var proxyManager *proxy.Manager
var controlHandler *controller.Handler
var cfg *config.ServerConfig

func init() {
	proxyManager = proxy.NewManager()
}

func main() {
	var err error

	var port *int = flag.Int("port", 9000, "Port to listen on")
	var configFile *string = flag.String("config", "configs/server.yaml", "Path to config file")
	flag.Parse()

	cfg, err = config.LoadServerConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	controlHandler = controller.NewHandler(proxyManager, cfg)

	var listener net.Listener
	tlsManager = tls.NewManager(cfg)
	listener, err = tlsManager.Listen(fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	// signals for shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	doneChan := make(chan struct{})

	go func() {

		log.Printf("Server listening on :%d", *port)

		for {
			acceptChan := make(chan net.Conn)
			acceptErrChan := make(chan error)

			go func() {
				conn, err := listener.Accept()
				if err != nil {
					acceptErrChan <- err
					return
				}
				acceptChan <- conn
			}()

			select { // for channel operations
			case <-doneChan:
				return
			case conn := <-acceptChan:
				log.Printf("New connection from %s", conn.RemoteAddr())

				var session *smux.Session
				session, err = smux.Server(conn, nil)
				if err != nil {
					log.Printf("Failed to create smux session: %v", err)
					conn.Close()
					continue
				}

				go serveClient(session)
			case err := <-acceptErrChan:
				if err != nil {
					select {
					case <-doneChan:
						return
					default:
						log.Printf("Failed to accept connection: %v", err)
					}
				}
			}
		}
	}()

	// Wait for termination signal (SIGINT or SIGTERM)
	<-sigChan
	log.Println("Shutting down server due to SIGINT or SIGTERM")
	close(doneChan)
	cleanup(listener)
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

func cleanup(listener net.Listener) {
	log.Println("Closing all proxy listeners...")
	proxyManager.CloseAllListeners()

	// Give ongoing connections time to complete
	shutdownTimer := time.NewTimer(5 * time.Second)
	shutdownComplete := make(chan struct{})

	go func() {
		log.Println("Closing main listener...")
		listener.Close()

		time.Sleep(1 * time.Second)
		close(shutdownComplete)
	}()

	// Wait for cleanup to complete or timeout
	select {
	case <-shutdownComplete:
		log.Println("Graceful shutdown completed")
	case <-shutdownTimer.C:
		log.Println("Shutdown timeout reached, forcing exit")
	}

	log.Println("Server stopped")
}
