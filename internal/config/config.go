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
	Port              string
	Host              string
	DBPath            string
	ServiceToken      string
	FolderID          string
	DriveAuthMode     string
	CredentialsFile   string
	OAuthClientID     string
	OAuthClientSecret string
	OAuthRefreshToken string
	AllowedUsers      []string
	AllowAnyUser      bool
	MaxImageBytes     int64
}

const (
	DriveAuthModeServiceAccount = "service_account"
	DriveAuthModeOAuth          = "oauth"
)

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("godotenv: %v", err)
	}

	cfg := &Config{
		Port:              getenv("PORT", "8090"),
		Host:              getenv("HOST", "127.0.0.1"),
		DBPath:            getenv("DRAWING_DB_PATH", "./drawing.db"),
		ServiceToken:      os.Getenv("DRAWING_SERVICE_TOKEN"),
		FolderID:          os.Getenv("GOOGLE_DRIVE_FOLDER_ID"),
		DriveAuthMode:     strings.ToLower(getenv("GOOGLE_DRIVE_AUTH_MODE", DriveAuthModeServiceAccount)),
		CredentialsFile:   os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON_PATH"),
		OAuthClientID:     os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		OAuthClientSecret: os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		OAuthRefreshToken: os.Getenv("GOOGLE_OAUTH_REFRESH_TOKEN"),
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
	switch cfg.DriveAuthMode {
	case DriveAuthModeServiceAccount:
		if cfg.CredentialsFile == "" {
			return nil, errors.New("GOOGLE_SERVICE_ACCOUNT_JSON_PATH is required")
		}
	case DriveAuthModeOAuth:
		if cfg.OAuthClientID == "" {
			return nil, errors.New("GOOGLE_OAUTH_CLIENT_ID is required")
		}
		if cfg.OAuthClientSecret == "" {
			return nil, errors.New("GOOGLE_OAUTH_CLIENT_SECRET is required")
		}
		if cfg.OAuthRefreshToken == "" {
			return nil, errors.New("GOOGLE_OAUTH_REFRESH_TOKEN is required")
		}
	default:
		return nil, fmt.Errorf("GOOGLE_DRIVE_AUTH_MODE must be %q or %q", DriveAuthModeServiceAccount, DriveAuthModeOAuth)
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
