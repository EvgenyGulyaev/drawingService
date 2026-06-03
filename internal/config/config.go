package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port            string
	Host            string
	DBPath          string
	ServiceToken    string
	FolderID        string
	CredentialsFile string
	AllowedUsers    []string
	AllowAnyUser    bool
	CanvasWidth     int
	CanvasHeight    int
	MaxImageBytes   int64
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("godotenv: %v", err)
	}

	cfg := &Config{
		Port:            getenv("PORT", "8090"),
		Host:            getenv("HOST", "127.0.0.1"),
		DBPath:          getenv("DRAWING_DB_PATH", "./drawing.db"),
		ServiceToken:    os.Getenv("DRAWING_SERVICE_TOKEN"),
		FolderID:        os.Getenv("GOOGLE_DRIVE_FOLDER_ID"),
		CredentialsFile: os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON_PATH"),
	}

	allowed := getenv("DRAWING_ALLOWED_USERS", "*")
	for _, entry := range strings.Split(allowed, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if entry == "*" {
			cfg.AllowAnyUser = true
		}
		cfg.AllowedUsers = append(cfg.AllowedUsers, entry)
	}
	if allowed == "" {
		return nil, errors.New("DRAWING_ALLOWED_USERS is required")
	}

	canvasWidth, err := strconv.Atoi(getenv("DRAWING_CANVAS_WIDTH", "1600"))
	if err != nil || canvasWidth <= 0 {
		return nil, fmt.Errorf("DRAWING_CANVAS_WIDTH must be a positive integer")
	}
	cfg.CanvasWidth = canvasWidth

	canvasHeight, err := strconv.Atoi(getenv("DRAWING_CANVAS_HEIGHT", "1000"))
	if err != nil || canvasHeight <= 0 {
		return nil, fmt.Errorf("DRAWING_CANVAS_HEIGHT must be a positive integer")
	}
	cfg.CanvasHeight = canvasHeight

	maxBytes, err := strconv.ParseInt(getenv("MAX_IMAGE_BYTES", "10485760"), 10, 64)
	if err != nil || maxBytes <= 0 {
		return nil, fmt.Errorf("MAX_IMAGE_BYTES must be a positive integer")
	}
	cfg.MaxImageBytes = maxBytes

	if cfg.ServiceToken == "" {
		return nil, errors.New("DRAWING_SERVICE_TOKEN is required")
	}
	if cfg.FolderID == "" {
		return nil, errors.New("GOOGLE_DRIVE_FOLDER_ID is required")
	}
	if cfg.CredentialsFile == "" {
		return nil, errors.New("GOOGLE_SERVICE_ACCOUNT_JSON_PATH is required")
	}
	return cfg, nil
}

func (c *Config) ListenAddr() string {
	return c.Host + ":" + c.Port
}

func getenv(key, def string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	return value
}
