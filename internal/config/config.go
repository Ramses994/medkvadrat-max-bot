package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	MaxBotToken  string
	GatewayURL   string
	GatewayToken string
	DBPath       string

	KeyboardDebug bool
}

func Load() (*Config, error) {
	// В проде переменные приходят от docker/systemd, .env только для локалки
	_ = godotenv.Load()

	cfg := &Config{
		MaxBotToken:  os.Getenv("MAX_BOT_TOKEN"),
		GatewayURL:   os.Getenv("GATEWAY_URL"),
		GatewayToken: os.Getenv("GATEWAY_TOKEN"),
		DBPath:       os.Getenv("DB_PATH"),
	}

	if cfg.MaxBotToken == "" {
		return nil, fmt.Errorf("не задан MAX_BOT_TOKEN")
	}
	if cfg.GatewayURL == "" {
		return nil, fmt.Errorf("не задан GATEWAY_URL")
	}
	if cfg.GatewayToken == "" {
		return nil, fmt.Errorf("не задан GATEWAY_TOKEN")
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "./data/bot.db"
	}

	cfg.KeyboardDebug = envBool("KEYBOARD_DEBUG", false)

	return cfg, nil
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
