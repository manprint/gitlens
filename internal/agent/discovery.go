package agent

import "context"

// Discovery runs periodically to find databases on a target.
// It is called by the scheduler every 5 minutes via Manager.Discover().
type Discovery struct{}

// Run executes discovery via a Manager's Discover() method.
func (d *Discovery) Run(ctx context.Context, m *Manager) error {
	_, err := m.Discover(ctx)
	return err
}
