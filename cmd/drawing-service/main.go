package main

import (
	"context"
	"log"
	"time"

	"drawingService/internal/config"
	"drawingService/internal/game"
	"drawingService/internal/google"
	httpserver "drawingService/internal/http"
	"drawingService/internal/service"
	"drawingService/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := store.OpenDb(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("close db: %v", err)
		}
	}()
	repo := store.NewDrawingRepository(db)
	if err := repo.EnsureBuckets(); err != nil {
		log.Fatalf("ensure buckets: %v", err)
	}

	drive, err := google.NewDriveStorage(context.Background(), google.DriveOptions{
		FolderID:          cfg.FolderID,
		CredentialsFile:   cfg.CredentialsFile,
		OAuthClientID:     cfg.OAuthClientID,
		OAuthClientSecret: cfg.OAuthClientSecret,
		OAuthRefreshToken: cfg.OAuthRefreshToken,
	})
	if err != nil {
		log.Fatalf("init drive storage: %v", err)
	}
	stampDrive, err := google.NewDriveStorage(context.Background(), google.DriveOptions{
		FolderID:          cfg.StampsFolderID,
		CredentialsFile:   cfg.CredentialsFile,
		OAuthClientID:     cfg.OAuthClientID,
		OAuthClientSecret: cfg.OAuthClientSecret,
		OAuthRefreshToken: cfg.OAuthRefreshToken,
	})
	if err != nil {
		log.Fatalf("init stamp drive storage: %v", err)
	}

	svc := service.NewDrawingService(repo, drive, cfg.MaxImageBytes).
		WithStampStorage(stampDrive).
		WithStampLimits(cfg.MaxStampImageBytes, cfg.MaxStampImageDimension)
	auth := httpserver.AuthConfig{
		ServiceToken: cfg.ServiceToken,
		AllowedUsers: cfg.AllowedUsers,
		AllowAnyUser: cfg.AllowAnyUser,
	}
	gameHandler, err := game.New(db.DB)
	if err != nil {
		log.Fatalf("init game: %v", err)
	}
	go func() {
		for now := range time.NewTicker(time.Minute).C {
			if err := gameHandler.Cleanup(now); err != nil {
				log.Printf("game cleanup: %v", err)
			}
		}
	}()
	handler := httpserver.NewHandler(auth, svc, drive).WithGame(gameHandler)
	server := httpserver.NewServer(cfg.ListenAddr(), handler)

	if err := server.Start(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
