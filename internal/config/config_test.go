package config

import "testing"

func TestLoadAllowsOAuthDriveAuthWithoutServiceAccountFile(t *testing.T) {
	t.Setenv("PORT", "8090")
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("DRAWING_DB_PATH", "./drawing.db")
	t.Setenv("DRAWING_SERVICE_TOKEN", "service-token")
	t.Setenv("GOOGLE_DRIVE_FOLDER_ID", "folder-id")
	t.Setenv("GOOGLE_DRIVE_AUTH_MODE", "oauth")
	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "client-secret")
	t.Setenv("GOOGLE_OAUTH_REFRESH_TOKEN", "refresh-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.DriveAuthMode != DriveAuthModeOAuth {
		t.Fatalf("expected oauth auth mode, got %q", cfg.DriveAuthMode)
	}
	if cfg.OAuthClientID != "client-id" {
		t.Fatalf("expected oauth client id")
	}
	if cfg.OAuthClientSecret != "client-secret" {
		t.Fatalf("expected oauth client secret")
	}
	if cfg.OAuthRefreshToken != "refresh-token" {
		t.Fatalf("expected oauth refresh token")
	}
	if cfg.CredentialsFile != "" {
		t.Fatalf("oauth auth should not require service account file, got %q", cfg.CredentialsFile)
	}
}

func TestLoadRejectsOAuthDriveAuthWithoutRefreshToken(t *testing.T) {
	t.Setenv("DRAWING_SERVICE_TOKEN", "service-token")
	t.Setenv("GOOGLE_DRIVE_FOLDER_ID", "folder-id")
	t.Setenv("GOOGLE_DRIVE_AUTH_MODE", "oauth")
	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "client-secret")

	if _, err := Load(); err == nil {
		t.Fatalf("expected missing refresh token error")
	}
}
