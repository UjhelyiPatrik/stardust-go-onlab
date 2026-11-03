package satellite

import (
	"math"
	"testing"
	"time"
)

func TestParseEpoch(t *testing.T) {
	tests := []struct {
		name        string
		epochStr    string
		expected    time.Time
		expectError bool
	}{
		{
			name:        "Day 1 of 2025 (start of year)",
			epochStr:    "25001.00000000",
			expected:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			expectError: false,
		},
		{
			name:        "Day 1.5 of 2025 (noon Jan 1)",
			epochStr:    "25001.50000000",
			expected:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
			expectError: false,
		},
		{
			name:        "Day 32 of 2024 (Feb 1)",
			epochStr:    "24032.00000000",
			expected:    time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			expectError: false,
		},
		{
			name:        "Invalid short string",
			epochStr:    "251",
			expectError: true,
		},
		{
			name:        "Invalid non-numeric string",
			epochStr:    "XX999.00000000",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := parseEpoch(tt.epochStr)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for %s, got nil", tt.epochStr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", tt.epochStr, err)
			}
			diff := res.Sub(tt.expected)
			if math.Abs(diff.Seconds()) > 1.0 {
				t.Errorf("expected %v, got %v (diff %v)", tt.expected, res, diff)
			}
		})
	}
}
