package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	log.Printf("Connecting to server at %s", config.Server)
	var conn net.Conn
	conn, err = net.Dial("tcp", config.Server)
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	var session *smux.Session
	session, err = smux.Client(conn, nil)
	if err != nil {
		log.Fatalf("Failed to create smux session: %v", err)
	}
	defer session.Close()

	var proxyHandler *proxy.Handler = proxy.NewHandler(session, config)

	var ctrlStream *smux.Stream
	ctrlStream, err = session.OpenStream()
	if err != nil {
		log.Fatalf("Failed to open control stream: %v", err)
	}
	defer ctrlStream.Close()

	proxyHandler.RegisterProxies(ctrlStream)

	var testStream *smux.Stream
	testStream, err = session.OpenStream()
	if err != nil {
		log.Fatalf("Failed to open test stream: %v", err)
	}
	defer testStream.Close()

	go sendTestMessages(testStream)
	go receiveResponses(testStream)

	// Set up signal handling for clean shutdown
	var sigChan chan os.Signal = make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go acceptStreams(session, proxyHandler)

	// Wait for termination signal
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

func sendTestMessages(stream *smux.Stream) {
	count := 1
	for {
		message := fmt.Sprintf("Test message #%d from client", count)
		_, err := stream.Write([]byte(message))
		if err != nil {
			log.Printf("Failed to send test message: %v", err)
			return
		}
		log.Printf("Sent: %s", message)
		count++
		time.Sleep(5 * time.Second)
	}
}

func receiveResponses(stream *smux.Stream) {
	var err error
	var buffer []byte = make([]byte, 1024)
	for {
		var n int
		n, err = stream.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from stream: %v", err)
			}
			return
		}
		var message string = string(buffer[:n])
		log.Printf("Received: %s", message)
	}
}
