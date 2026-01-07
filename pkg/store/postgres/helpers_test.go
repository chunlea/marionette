package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeCursor(t *testing.T) {
	ts := time.Date(2024, 1, 15, 10, 30, 0, 123456789, time.UTC)
	id := "alog_test123"

	cursor := encodeCursor(ts, id)

	// Should be non-empty base64 string
	assert.NotEmpty(t, cursor)

	// Should be decodable
	decodedTime, decodedID, err := decodeCursor(cursor)
	require.NoError(t, err)
	assert.Equal(t, ts, decodedTime)
	assert.Equal(t, id, decodedID)
}

func TestDecodeCursor(t *testing.T) {
	t.Run("empty cursor", func(t *testing.T) {
		ts, id, err := decodeCursor("")
		require.NoError(t, err)
		assert.True(t, ts.IsZero())
		assert.Empty(t, id)
	})

	t.Run("valid cursor", func(t *testing.T) {
		// Create a known cursor
		originalTime := time.Date(2024, 6, 15, 14, 30, 45, 123456789, time.UTC)
		originalID := "alog_abc123"
		cursor := encodeCursor(originalTime, originalID)

		ts, id, err := decodeCursor(cursor)
		require.NoError(t, err)
		assert.Equal(t, originalTime, ts)
		assert.Equal(t, originalID, id)
	})

	t.Run("invalid base64", func(t *testing.T) {
		_, _, err := decodeCursor("not-valid-base64!!!")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid cursor encoding")
	})

	t.Run("invalid format - no separator", func(t *testing.T) {
		// Base64 encode a string without the | separator
		cursor := "bm9zZXBhcmF0b3I=" // "noseparator" in base64
		_, _, err := decodeCursor(cursor)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid cursor format")
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		// Base64 encode "invalid-time|id123"
		cursor := "aW52YWxpZC10aW1lfGlkMTIz" // "invalid-time|id123" in base64
		_, _, err := decodeCursor(cursor)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid cursor timestamp")
	})
}

func TestCursorRoundTrip(t *testing.T) {
	testCases := []struct {
		name string
		ts   time.Time
		id   string
	}{
		{
			name: "normal case",
			ts:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			id:   "alog_123",
		},
		{
			name: "with nanoseconds",
			ts:   time.Date(2024, 12, 31, 23, 59, 59, 999999999, time.UTC),
			id:   "alog_xyz789",
		},
		{
			name: "empty id",
			ts:   time.Now().UTC().Truncate(time.Nanosecond),
			id:   "",
		},
		{
			name: "id with special chars",
			ts:   time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
			id:   "alog_test-123_abc",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cursor := encodeCursor(tc.ts, tc.id)
			decodedTime, decodedID, err := decodeCursor(cursor)

			require.NoError(t, err)
			assert.Equal(t, tc.ts, decodedTime)
			assert.Equal(t, tc.id, decodedID)
		})
	}
}
