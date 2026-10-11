package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadProxy reads a ProxyConfig from a YAML file path.
func LoadProxy(path string) (ProxyConfig, error) {
	var cfg ProxyConfig
	if err := loadYAML(path, &cfg); err != nil {
		return ProxyConfig{}, fmt.Errorf("load proxy config: %w", err)
	}
	return cfg, nil
}

// LoadEvolve reads an EvolveConfig from a YAML file path.
func LoadEvolve(path string) (EvolveConfig, error) {
	var cfg EvolveConfig
	if err := loadYAML(path, &cfg); err != nil {
		return EvolveConfig{}, fmt.Errorf("load evolve config: %w", err)
	}
	return cfg, nil
}

func loadYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, out)
}
