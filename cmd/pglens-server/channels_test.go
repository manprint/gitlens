package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/server"
)

func channelNames(cfg server.AlertConfig) []string {
	names := []string{}
	for _, c := range buildChannels(cfg) {
		names = append(names, c.Name())
	}
	return names
}

func TestCONFIGGC002_GoogleChatChannelOnlyWhenConfigured(t *testing.T) {
	require.Empty(t, channelNames(server.AlertConfig{}))
	require.Equal(t, []string{"google_chat"}, channelNames(server.AlertConfig{GoogleChatURL: "https://chat.example/x"}))
	require.Equal(t, []string{"slack", "webhook", "google_chat"},
		channelNames(server.AlertConfig{SlackURL: "s", WebhookURL: "w", GoogleChatURL: "g"}))
	require.Equal(t, []string{"slack"}, channelNames(server.AlertConfig{SlackURL: "s"}))
}
