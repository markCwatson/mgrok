package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ServerConfig holds the server configuration
type ServerConfig struct {
	EnableTLS      bool   `yaml:"enable_tls"`
	TLSCertFile    string `yaml:"tls_cert_file"`
	TLSKeyFile     string `yaml:"tls_key_file"`
	BindAddr       string `yaml:"bind_addr"`
	BindPort       int    `yaml:"bind_port"`
	AuthToken      string `yaml:"auth_token"`
	PortRangeStart int    `yaml:"port_range_start"`
	PortRangeEnd   int    `yaml:"port_range_end"`
}

// LoadServerConfig loads the server configuration from a YAML file
func LoadServerConfig(configPath string) (*ServerConfig, error) {
	// Expand home directory if needed
	if configPath[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configPath = filepath.Join(home, configPath[2:])
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config ServerConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// Expand home directory in file paths if needed
	if config.TLSCertFile[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		config.TLSCertFile = filepath.Join(home, config.TLSCertFile[2:])
	}

	if config.TLSKeyFile[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		config.TLSKeyFile = filepath.Join(home, config.TLSKeyFile[2:])
	}

	return &config, nil
}
