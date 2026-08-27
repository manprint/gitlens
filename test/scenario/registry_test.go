//go:build e2e

package scenario

import (
	"context"
	"testing"
	"time"
)

func TestRegistry_DuplicateIDPanics(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	// Register a scenario
	s1 := Scenario{
		ID:     "TEST-001",
		Title:  "First test",
		Smoke:  true,
		Covers: []string{"test"},
		Run: func(ctx context.Context, e *Env) error {
			return nil
		},
		Expect: Expectations{
			Events: []string{"test_event"},
		},
	}
	Register(s1)

	// Attempt to register another with the same ID should panic
	s2 := Scenario{
		ID:     "TEST-001",
		Title:  "Duplicate ID",
		Smoke:  false,
		Covers: []string{"test"},
		Run: func(ctx context.Context, e *Env) error {
			return nil
		},
		Expect: Expectations{
			Events: []string{"other_event"},
		},
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate ID, but got none")
		}
	}()
	Register(s2)
}

func TestRegistry_EmptyIDPanics(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	s := Scenario{
		ID:     "",
		Title:  "Empty ID",
		Smoke:  false,
		Covers: []string{"test"},
		Run: func(ctx context.Context, e *Env) error {
			return nil
		},
		Expect: Expectations{
			Events: []string{"event"},
		},
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty ID, but got none")
		}
	}()
	Register(s)
}

func TestRegistry_NilRunPanics(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	s := Scenario{
		ID:     "TEST-002",
		Title:  "Nil Run",
		Smoke:  false,
		Covers: []string{"test"},
		Run:    nil, // Invalid
		Expect: Expectations{
			Events: []string{"event"},
		},
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil Run, but got none")
		}
	}()
	Register(s)
}

func TestRegistry_MissingCoversPanics(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	s := Scenario{
		ID:     "TEST-003",
		Title:  "Missing Covers",
		Smoke:  false,
		Covers: []string{}, // Empty
		Run: func(ctx context.Context, e *Env) error {
			return nil
		},
		Expect: Expectations{
			Events: []string{"event"},
		},
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on missing Covers, but got none")
		}
	}()
	Register(s)
}

func TestRegistry_MissingExpectationsPanics(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	s := Scenario{
		ID:     "TEST-004",
		Title:  "Missing Expectations",
		Smoke:  false,
		Covers: []string{"test"},
		Run: func(ctx context.Context, e *Env) error {
			return nil
		},
		Expect: Expectations{
			// Both Events and Invariants are empty
		},
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on missing expectations, but got none")
		}
	}()
	Register(s)
}

func TestRegistry_ExpectInvariantsSufficesIfEventsEmpty(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	s := Scenario{
		ID:     "TEST-005",
		Title:  "Invariants Only",
		Smoke:  false,
		Covers: []string{"test"},
		Run: func(ctx context.Context, e *Env) error {
			return nil
		},
		Expect: Expectations{
			Invariants: []string{"I-1"},
		},
	}

	// Should not panic
	Register(s)
	if found, ok := Get("TEST-005"); !ok || found.ID != "TEST-005" {
		t.Fatal("failed to retrieve registered scenario")
	}
}

func TestRegistry_Get(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	s := Scenario{
		ID:     "TEST-006",
		Title:  "Get Test",
		Smoke:  true,
		Covers: []string{"I-1"},
		Run: func(ctx context.Context, e *Env) error {
			return nil
		},
		Expect: Expectations{
			Events: []string{"cluster_created"},
		},
	}
	Register(s)

	found, ok := Get("TEST-006")
	if !ok {
		t.Fatal("Get should return true for registered scenario")
	}
	if found.ID != "TEST-006" {
		t.Errorf("expected ID TEST-006, got %s", found.ID)
	}
	if found.Title != "Get Test" {
		t.Errorf("expected title 'Get Test', got %s", found.Title)
	}
	if !found.Smoke {
		t.Error("expected Smoke=true")
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	_, ok := Get("NONEXISTENT")
	if ok {
		t.Fatal("Get should return false for nonexistent scenario")
	}
}

func TestRegistry_All(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	ids := []string{"B", "A", "C"}
	for _, id := range ids {
		s := Scenario{
			ID:     id,
			Title:  id,
			Smoke:  false,
			Covers: []string{"test"},
			Run: func(ctx context.Context, e *Env) error {
				return nil
			},
			Expect: Expectations{
				Events: []string{"event"},
			},
		}
		Register(s)
	}

	all := All()
	if len(all) != 3 {
		t.Fatalf("expected 3 scenarios, got %d", len(all))
	}

	// Verify sorted order
	expected := []string{"A", "B", "C"}
	for i, scenario := range all {
		if scenario.ID != expected[i] {
			t.Errorf("position %d: expected ID %s, got %s", i, expected[i], scenario.ID)
		}
	}
}

func TestRegistry_AllEmpty(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	all := All()
	if len(all) != 0 {
		t.Fatalf("expected empty slice, got %d scenarios", len(all))
	}
}

func TestRegistry_Smoke(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	scenarios := []struct {
		id    string
		smoke bool
	}{
		{"SYS-A", true},
		{"SYS-B", false},
		{"SYS-C", true},
		{"SYS-D", false},
	}

	for _, s := range scenarios {
		scenario := Scenario{
			ID:     s.id,
			Title:  s.id,
			Smoke:  s.smoke,
			Covers: []string{"test"},
			Run: func(ctx context.Context, e *Env) error {
				return nil
			},
			Expect: Expectations{
				Events: []string{"event"},
			},
		}
		Register(scenario)
	}

	smoke := Smoke()
	if len(smoke) != 2 {
		t.Fatalf("expected 2 smoke scenarios, got %d", len(smoke))
	}

	// Verify all returned are smoke
	for _, s := range smoke {
		if !s.Smoke {
			t.Errorf("Smoke() returned non-smoke scenario %s", s.ID)
		}
	}

	// Verify sorted
	if smoke[0].ID > smoke[1].ID {
		t.Error("Smoke() results should be sorted by ID")
	}
}

func TestRegistry_SmokeEmpty(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	// Register only non-smoke scenarios
	s := Scenario{
		ID:     "TEST-007",
		Title:  "Non-smoke",
		Smoke:  false,
		Covers: []string{"test"},
		Run: func(ctx context.Context, e *Env) error {
			return nil
		},
		Expect: Expectations{
			Events: []string{"event"},
		},
	}
	Register(s)

	smoke := Smoke()
	if len(smoke) != 0 {
		t.Fatalf("expected no smoke scenarios, got %d", len(smoke))
	}
}

func TestRegistry_AllScenariosHaveCoversAndExpectations(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	// Register scenarios with valid Covers and Expectations
	testCases := []struct {
		id     string
		covers []string
		events []string
		invars []string
	}{
		{"TEST-008", []string{"I-1"}, []string{"event1"}, nil},
		{"TEST-009", []string{"I-1", "I-2"}, nil, []string{"invariant1"}},
		{"TEST-010", []string{"IDEA.md#5.1"}, []string{"event2"}, []string{"invariant2"}},
	}

	for _, tc := range testCases {
		s := Scenario{
			ID:     tc.id,
			Title:  tc.id,
			Smoke:  false,
			Covers: tc.covers,
			Run: func(ctx context.Context, e *Env) error {
				return nil
			},
			Expect: Expectations{
				Events:     tc.events,
				Invariants: tc.invars,
			},
		}
		Register(s)
	}

	// Verify all registered scenarios have Covers and Expectations
	all := All()
	for _, s := range all {
		if len(s.Covers) == 0 {
			t.Errorf("Scenario %s has no Covers", s.ID)
		}
		if len(s.Expect.Events) == 0 && len(s.Expect.Invariants) == 0 {
			t.Errorf("Scenario %s has no expectations (events or invariants)", s.ID)
		}
	}
}

func TestRegistry_ConcurrentRegisterAndGet(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	// Test concurrent operations don't cause data races
	done := make(chan bool, 10)

	// Gorine 1: Register scenarios
	go func() {
		for i := 0; i < 5; i++ {
			s := Scenario{
				ID:     "CONC-" + string(rune('A'+i)),
				Title:  "Concurrent test",
				Smoke:  i%2 == 0,
				Covers: []string{"test"},
				Run: func(ctx context.Context, e *Env) error {
					return nil
				},
				Expect: Expectations{
					Events: []string{"event"},
				},
			}
			Register(s)
		}
		done <- true
	}()

	// Goroutines 2-5: Read scenarios
	for i := 0; i < 4; i++ {
		go func() {
			time.Sleep(time.Millisecond) // Small delay to ensure overlap
			_ = All()
			_ = Smoke()
			done <- true
		}()
	}

	// Wait for all to complete
	for i := 0; i < 5; i++ {
		<-done
	}
}

func TestRegistry_ScenarioDefaults(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	s := Scenario{
		ID:       "TEST-011",
		Title:    "Test defaults",
		Topology: TopologyStandalone,
		Covers:   []string{"I-1"},
		Run: func(ctx context.Context, e *Env) error {
			return nil
		},
		Expect: Expectations{
			Events: []string{"event"},
		},
		// Smoke defaults to false, EstDuration defaults to 0
	}
	Register(s)

	found, _ := Get("TEST-011")
	if found.Smoke {
		t.Error("expected Smoke to default to false")
	}
	if found.EstDuration != 0 {
		t.Error("expected EstDuration to default to 0")
	}
	if found.Topology != TopologyStandalone {
		t.Error("expected Topology to be preserved")
	}
}

func TestRegistry_EstDurationPreserved(t *testing.T) {
	t.Helper()
	Reset()
	defer Reset()

	s := Scenario{
		ID:          "TEST-012",
		Title:       "Duration test",
		EstDuration: 2 * time.Minute,
		Covers:      []string{"I-1"},
		Run: func(ctx context.Context, e *Env) error {
			return nil
		},
		Expect: Expectations{
			Events: []string{"event"},
		},
	}
	Register(s)

	found, _ := Get("TEST-012")
	if found.EstDuration != 2*time.Minute {
		t.Errorf("expected EstDuration 2m, got %v", found.EstDuration)
	}
}
