package config

import (
	"os"
	"path/filepath"

	"github.com/fjacquet/idrac_exporter/internal/log"
	"github.com/joho/godotenv"
)

// LoadDotEnv loads .env files BEFORE the config's ${VAR} interpolation so those
// references resolve. It checks the current directory first, then the config
// file's directory. godotenv.Load never overrides an already-set environment
// variable, so real secret injection always wins (.env is nice, env is the way).
func LoadDotEnv(cfgPath string) {
	candidates := []string{".env"}
	if cfgPath != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(cfgPath), ".env"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := godotenv.Load(p); err != nil {
			log.Warn("Failed to load %s: %v", p, err)
			continue
		}
		log.Info("Loaded environment from %s", p)
	}
}
