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

	"github.com/xtaci/smux"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  string `yaml:"server"`
	Token   string `yaml:"token"`
	Proxies map[string]struct {
		Type       string `yaml:"type"`
		LocalPort  int    `yaml:"local_port"`
		RemotePort int    `yaml:"remote_port"`
	} `yaml:"proxies"`
}

func main() {
	configPath := flag.String("config", "configs/client.yaml", "Path to config file")
	serverAddr := flag.String("server", "", "Server address (overrides config file)")
	flag.Parse()

	// Load configuration
	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Override server address if provided via command line
	if *serverAddr != "" {
		config.Server = *serverAddr
	}

	// Use localhost:9000 if not specified
	if config.Server == "" {
		config.Server = "localhost:9000"
	}

	// Connect to server with plain TCP for development
	log.Printf("Connecting to server at %s", config.Server)
	conn, err := net.Dial("tcp", config.Server)
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	// Set up multiplexing
	session, err := smux.Client(conn, nil)
	if err != nil {
		log.Fatalf("Failed to create smux session: %v", err)
	}
	defer session.Close()

	// Open control stream
	ctrlStream, err := session.OpenStream()
	if err != nil {
		log.Fatalf("Failed to open control stream: %v", err)
	}
	defer ctrlStream.Close()

	// Send proxy registrations
	registerProxies(ctrlStream, config)

	// Open a test stream for sending messages
	testStream, err := session.OpenStream()
	if err != nil {
		log.Fatalf("Failed to open test stream: %v", err)
	}
	defer testStream.Close()

	// Start a goroutine to send test messages
	go sendTestMessages(testStream)
	// Start a goroutine to receive responses
	go receiveResponses(testStream)

	// Set up signal handling for clean shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Accept streams from the server
	go acceptStreams(session)

	// Wait for termination signal
	<-sigChan
	log.Println("Shutting down client...")
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

func registerProxies(conn net.Conn, config *Config) {
	// TODO: Implement proxy registration using the protocol defined in the README
	log.Println("Registering proxies (not yet implemented)")
	// For each proxy in config.Proxies, register it with the server
}

func acceptStreams(session *smux.Session) {
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			log.Printf("Failed to accept stream: %v", err)
			return
		}

		go handleStream(stream)
	}
}

func handleStream(stream *smux.Stream) {
	defer stream.Close()

	// TODO: Implement stream handling
	// 1. Determine which local service this stream is for
	// 2. Connect to that local service
	// 3. Copy data in both directions

	// This is just a placeholder
	log.Printf("Received stream %d (not yet handling)", stream.ID())
	
	// Discard all data from the stream for now
	io.Copy(io.Discard, stream)
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
	buffer := make([]byte, 1024)
	for {
		n, err := stream.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from stream: %v", err)
			}
			return
		}
		message := string(buffer[:n])
		log.Printf("Received: %s", message)
	}
} 