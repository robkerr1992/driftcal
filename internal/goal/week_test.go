package goal

import (
	"testing"
	"time"
)

func TestMondayOf(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "Monday input",
			input:    time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC),
			expected: time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "Sunday input",
			input:    time.Date(2026, 3, 1, 15, 30, 0, 0, time.UTC),
			expected: time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "Mid-week Wednesday",
			input:    time.Date(2026, 2, 25, 8, 0, 0, 0, time.UTC),
			expected: time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "Year boundary Thursday",
			input:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MondayOf(tt.input)
			if !got.Equal(tt.expected) {
				t.Errorf("MondayOf(%v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
