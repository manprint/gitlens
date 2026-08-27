package topology

// Provider turns one instance's check results into topology facts.
// Only `streaming` exists in this plan (decision D6); Aurora, Patroni and Citus
// become additional providers without touching the engine or the schema.
type Provider interface {
	Name() string
	Applies(inst Observation) bool
	Edges(inst Observation) []Edge
}

// StreamingProvider extracts edges from streaming replication observations.
type StreamingProvider struct{}

// Name returns the provider name.
func (p *StreamingProvider) Name() string {
	return "streaming"
}

// Applies returns true if this observation contains streaming replication info.
func (p *StreamingProvider) Applies(inst Observation) bool {
	// Applies to any instance; edges may be empty
	return true
}

// Edges extracts edges from the observation (already provided by the caller).
func (p *StreamingProvider) Edges(inst Observation) []Edge {
	return inst.Edges
}
