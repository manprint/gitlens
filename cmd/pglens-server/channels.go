package main

import (
	"github.com/manprint/pglens/internal/alert/notify"
	"github.com/manprint/pglens/internal/server"
)

// buildChannels returns one notification channel per configured destination.
func buildChannels(cfg server.AlertConfig) []notify.Channel {
	channels := []notify.Channel{}
	if cfg.SlackURL != "" {
		channels = append(channels, notify.NewSlack(cfg.SlackURL, nil))
	}
	if cfg.WebhookURL != "" {
		channels = append(channels, notify.NewWebhook(cfg.WebhookURL, nil))
	}
	if cfg.GoogleChatURL != "" {
		channels = append(channels, notify.NewGoogleChat(cfg.GoogleChatURL, nil))
	}
	return channels
}

// wireNotificationChannels builds the delivery channels once, without the
// database pool, and gives the same instances to the console's test send
// (11.5a, N3). The alert notifier is built over the returned channels.
func wireNotificationChannels(api *server.API, cfg server.AlertConfig) []notify.Channel {
	channels := buildChannels(cfg)
	api.SetNotificationChannels(cfg, channels)
	return channels
}
