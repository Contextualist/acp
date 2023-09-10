package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSetup(t *testing.T) {
	_ = os.Remove(configFilename)
	if _, err := getConfig(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Config exists before creation; err: %v", err)
	}
	if err := Setup(""); err != nil {
		t.Fatalf("Config initialization failed: %v", err)
	}
	conf, err := getConfig()
	if err != nil {
		t.Fatalf("Failed to get initialized config: %v", err)
	}
	if conf.ID == "" || conf.PSK == "" {
		t.Fatalf("Initialized config does not have all necessary fields: %+v", conf)
	}
}

func TestSetupWith(t *testing.T) {
	conf0 := Config{
		ID:      "AAAAAAAA",
		PSK:     "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		Server:  "http://localhost:8000",
		UseIPv6: true,
		Ports:   []int{0, 9527},
	}
	conf0Bytes, _ := json.Marshal(&conf0)
	if err := Setup(string(conf0Bytes)); err != nil {
		t.Fatalf("Config override failed: %v", err)
	}
	conf, err := getConfig()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}
	if !reflect.DeepEqual(*conf, conf0) {
		t.Fatalf("Config does not match the intented setup value: expect: %+v, got: %+v", conf0, conf)
	}

	if err := Setup(`{"ipv6":"true"}`); err == nil {
		t.Fatalf("Setup failed to catch an invalid input")
	}
}

func TestMain(m *testing.M) {
	configFilename = filepath.Join(os.TempDir(), "acp-test-config.json")
	rc := m.Run()
	os.Remove(configFilename)
	os.Exit(rc)
}
