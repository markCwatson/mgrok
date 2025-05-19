package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/markCwatson/mgrok/internal/client/proxy"
	"github.com/xtaci/smux"
	"gopkg.in/yaml.v3"
)

func main() {
	var err error

	configPath := flag.String("config", "configs/client.yaml", "Path to config file")
	serverAddr := flag.String("server", "", "Server address (overrides config file)")
	flag.Parse()

	var config *proxy.Config
	config, err = loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *serverAddr != "" {
		config.Server = *serverAddr
	}

	if config.Server == "" {
		config.Server = "localhost:9000"
	}

	log.Printf("Connecting to server at %s using TLS", config.Server)
	var conn net.Conn
	conn, err = tls.Dial("tcp", config.Server, &tls.Config{
		ServerName: strings.Split(config.Server, ":")[0],
	})
	if err != nil {
		log.Printf("Failed to connect to server using TLS. Will try plain TCP.")
		conn, err = net.Dial("tcp", config.Server)
		if err != nil {
			log.Fatalf("Failed to connect to server using plain TCP: %v", err)
		}
	}
	defer conn.Close()

	var session *smux.Session
	session, err = smux.Client(conn, nil)
	if err != nil {
		log.Fatalf("Failed to create smux session: %v", err)
	}
	defer session.Close()

	var proxyHandler *proxy.Handler = proxy.NewHandler(session, config)

	// manages reg/heartbeat and stays open for the duration of the session
	var ctrlStream *smux.Stream
	ctrlStream, err = session.OpenStream()
	if err != nil {
		log.Fatalf("Failed to open control stream: %v", err)
	}
	defer ctrlStream.Close()

	proxyHandler.RegisterProxies(ctrlStream)

	// Set up signal handling for clean shutdown
	var sigChan chan os.Signal = make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go acceptStreams(session, proxyHandler)

	// Wait for termination signal (SIGINT or SIGTERM)
	<-sigChan
	log.Println("Shutting down client...")
}

func loadConfig(path string) (*proxy.Config, error) {
	var data []byte
	var err error
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config proxy.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

func acceptStreams(session *smux.Session, handler *proxy.Handler) {
	for {
		var stream *smux.Stream
		var err error
		stream, err = session.AcceptStream()
		if err != nil {
			log.Printf("Failed to accept stream: %v", err)
			return
		}

		go handler.HandleStream(stream)
	}
}
