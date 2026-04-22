package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	MaxBotToken  string // токен от @MasterBot
	GatewayURL   string // например http://localhost:8080
	GatewayToken string // тот же, что в API_TOKEN у api-gateway
	DBPath       string // путь к SQLite файлу
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

	return cfg, nil
}
