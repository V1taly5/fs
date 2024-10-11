package node

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Табличные тесты для функции toJson
func TestToJson(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
		wantErr  bool
	}{
		{
			name: "ValidStruct",
			input: struct {
				Name string `json:"name"`
			}{Name: "Alice"},
			expected: `{"name":"Alice"}`,
			wantErr:  false,
		},
		{
			name:     "MapInput",
			input:    map[string]int{"key": 1},
			expected: `{"key":1}`,
			wantErr:  false,
		},
		{
			name:     "NilInput",
			input:    nil,
			expected: `null`,
			wantErr:  false,
		},
		{
			name:     "ChannelInput", // Не сериализуемый тип
			input:    make(chan int),
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBytes := toJson(tt.input)
			if tt.wantErr {
				require.Nil(t, jsonBytes, "Expected nil result due to serialization error")
			} else {
				assert.NotNil(t, jsonBytes, "Expected non-nil result")
				actual := string(jsonBytes)
				assert.JSONEq(t, tt.expected, actual, "JSON output mismatch")
			}
		})
	}
}
