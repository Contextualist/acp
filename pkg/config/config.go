package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	tsapi "github.com/contextualist/acp/pkg/tailscale"
)

const (
	idLen  = 6  // 6 bytes, 8 base64 chars
	pskLen = 32 // for ChaCha20-Poly1305
)

// Config defines the user-specific information for the transfer.
// In general, it needs to be consistent across all devices of a user.
type Config struct {
	ID       string   `json:"id"`
	PSK      string   `json:"psk"`
	Server   string   `json:"server,omitempty"`
	UseIPv6  bool     `json:"ipv6,omitempty"`
	Ports    []int    `json:"ports,omitempty"`
	UPnP     bool     `json:"upnp,omitempty"`
	Strategy []string `json:"strategy,omitempty"`
}

func (conf *Config) ApplyDefault() {
	if conf.Server == "" {
		conf.Server = "https://acp.deno.dev"
	}
	if len(conf.Ports) == 0 {
		conf.Ports = []int{0}
	}
	if len(conf.Strategy) == 0 {
		conf.Strategy = []string{"tcp_punch"}
	}
}

var configFilename = filepath.Join(userConfigDir(), "acp", "config.json")

func Setup(confStr string) (err error) {
	var conf *Config
	if confStr != "" {
		conf = &Config{}
		if err = json.Unmarshal([]byte(confStr), conf); err != nil {
			return err
		}
		if len(conf.Strategy) == 0 {
			conf.Strategy = inferStrategy()
		}
		if err = setConfig(conf); err != nil {
			return err
		}
	} else {
		conf, err = getConfig()
		if errors.Is(err, os.ErrNotExist) {
			conf = &Config{
				ID:       base64.StdEncoding.EncodeToString(randBytes(idLen)),
				PSK:      base64.StdEncoding.EncodeToString(randBytes(pskLen)),
				Strategy: inferStrategy(),
			}
			if err = setConfig(conf); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		confBytes, _ := json.Marshal(&conf)
		confStr = string(confBytes)
	}
	conf.ApplyDefault()
	fmt.Printf(`acp is set up on this machine. To set up another machine, run the following command there
(DO NOT share the command publicly as it contains encryption keys)
	
    curl -fsS %s/get | sh -s -- --setup-with '%s'

(For Windows PowerShell, you need to download the executable to the Path manually)
If you already have the executable, run

    acp --setup-with '%s'

`, conf.Server, confStr, confStr)
	return nil
}

func MustGetConfig() *Config {
	conf, err := getConfig()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "Config not found. If this is your first time using acp, run `acp --setup` to generate a config")
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
	return conf
}

func getConfig() (*Config, error) {
	configFile, err := os.Open(configFilename)
	if err != nil {
		return nil, fmt.Errorf("error opening config: %w", err)
	}
	var conf Config
	err = json.NewDecoder(configFile).Decode(&conf)
	if err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}
	return &conf, nil
}

func setConfig(conf *Config) error {
	_ = os.MkdirAll(filepath.Dir(configFilename), 0o700)
	configFile, err := os.Create(configFilename)
	if err != nil {
		return fmt.Errorf("error writing config to %s: %v", configFilename, err)
	}
	err = json.NewEncoder(configFile).Encode(conf)
	if err != nil {
		return fmt.Errorf("error writing config to %s: %v", configFilename, err)
	}
	return nil
}

func inferStrategy() []string {
	_, iface, _ := tsapi.Interface()
	_, err := tsapi.Path()
	if iface != nil || err == nil {
		return []string{"tailscale", "tcp_punch"}
	}
	return nil
}

func userConfigDir() string {
	switch runtime.GOOS {
	case "linux", "darwin":
		return filepath.Join(os.Getenv("HOME"), ".config")
	case "windows":
		return os.Getenv("APPDATA")
	}
	fmt.Fprintf(os.Stderr, "OS %s is not supported\n", runtime.GOOS)
	os.Exit(1)
	return ""
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating random bytes: %v\n", err)
		os.Exit(1)
	}
	return b
}
