package main

import (
	"context"
	"log"

	"github.com/manprint/pglens/internal/alert/notify"
	"github.com/manprint/pglens/internal/server"
)

// buildChannels returns one notification channel per destination the
// environment configures.
func buildChannels(cfg server.AlertConfig) []notify.Channel {
	channels := []notify.Channel{}
	for _, kind := range []struct{ name, address string }{
		{"slack", cfg.SlackURL},
		{"webhook", cfg.WebhookURL},
		{"google_chat", cfg.GoogleChatURL},
	} {
		if kind.address != "" {
			channels = append(channels, server.NewNotificationChannel(kind.name, kind.address))
		}
	}
	return channels
}

// wireNotificationChannels builds the delivery channels of the environment
// once, applies the addresses stored from the console over them (11.5b), and
// returns the slot of every kind. The console's test send and the alert
// notifier deliver through the same slots (11.5a, N3), so an address set or
// removed from the console reaches the next alert without a restart. Without a
// database only the environment counts.
func wireNotificationChannels(ctx context.Context, api *server.API, cfg server.AlertConfig) []notify.Channel {
	api.SetNotificationChannels(cfg, buildChannels(cfg))
	if err := api.LoadNotificationChannels(ctx); err != nil {
		log.Printf("notifications: read the URLs stored from the console (using the environment's): %v", err)
	}
	slots := api.NotificationChannels()
	configured := 0
	for _, slot := range slots {
		if notify.Configured(slot) {
			configured++
		}
	}
	if configured == 0 {
		log.Println("no alert notification channel configured; alerts will be persisted but not delivered until one is set")
	}
	return slots
}
