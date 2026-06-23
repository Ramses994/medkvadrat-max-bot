package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	MaxBotToken  string
	GatewayURL   string
	GatewayToken string
	DBPath       string

	ReminderEnabled bool
	ReminderTick    time.Duration
	KeyboardDebug   bool
}

func Load() (*Config, error) {
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

	cfg.ReminderEnabled = envBoolDefaultTrue("REMINDER_ENABLED", true)
	tick, err := envDuration("REMINDER_TICK", 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("REMINDER_TICK: %w", err)
	}
	cfg.ReminderTick = tick
	cfg.KeyboardDebug = envBoolDefaultFalse("KEYBOARD_DEBUG", false)

	return cfg, nil
}

func envBoolDefaultTrue(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func envBoolDefaultFalse(key string, def bool) bool {
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

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, err
	}
	return d, nil
}
