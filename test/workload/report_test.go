package main

import (
	"encoding/json"
	"testing"
	"time"
)

// TestReport_JSONShape verifies that the Report structure marshals to JSON
// with the correct shape and types.
func TestReport_JSONShape(t *testing.T) {
	t.Run("report_marshals_to_json", func(t *testing.T) {
		now := time.Now()
		report := Report{
			Command:   "test-command",
			Seed:      12345,
			StartTime: now,
			EndTime:   now.Add(5 * time.Second),
			Duration:  5 * time.Second,
			Succeeded: 42,
			Failed:    3,
			Details: map[string]interface{}{
				"foo":    "bar",
				"count":  100,
				"ratio":  0.95,
				"nested": map[string]interface{}{"key": "value"},
			},
		}

		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		// Unmarshal to verify structure
		var unmarshaled map[string]interface{}
		err = json.Unmarshal(data, &unmarshaled)
		if err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		// Verify required fields exist and have correct types
		fields := map[string]string{
			"command":    "string",
			"seed":       "number",
			"start_time": "string",
			"end_time":   "string",
			"duration":   "number",
			"succeeded":  "number",
			"failed":     "number",
			"details":    "object",
		}

		for field, expectedType := range fields {
			val, exists := unmarshaled[field]
			if !exists {
				t.Errorf("field %q missing from JSON", field)
				continue
			}

			switch expectedType {
			case "string":
				if _, ok := val.(string); !ok {
					t.Errorf("field %q has type %T, want string", field, val)
				}
			case "number":
				if _, ok := val.(float64); !ok {
					t.Errorf("field %q has type %T, want number", field, val)
				}
			case "object":
				if _, ok := val.(map[string]interface{}); !ok {
					t.Errorf("field %q has type %T, want object", field, val)
				}
			}
		}
	})

	t.Run("report_with_empty_details", func(t *testing.T) {
		report := Report{
			Command:   "empty-test",
			Seed:      0,
			Succeeded: 0,
			Failed:    0,
			Details:   map[string]interface{}{},
		}

		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		// Should still unmarshal without error
		var unmarshaled Report
		err = json.Unmarshal(data, &unmarshaled)
		if err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		if unmarshaled.Command != "empty-test" {
			t.Errorf("command = %q, want %q", unmarshaled.Command, "empty-test")
		}
	})
}
