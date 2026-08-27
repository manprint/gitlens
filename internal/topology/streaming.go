package topology

// NewStreamingProvider creates a provider for streaming replication topology.
func NewStreamingProvider() Provider {
	return &StreamingProvider{}
}
