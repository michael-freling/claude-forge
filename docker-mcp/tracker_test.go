package main

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTracker_Add(t *testing.T) {
	tracker := NewTracker()

	tracker.Add("abc123", "my-container")

	assert.True(t, tracker.IsTracked("abc123"))
	assert.True(t, tracker.IsTracked("my-container"))
}

func TestTracker_Remove(t *testing.T) {
	tracker := NewTracker()

	tracker.Add("abc123", "my-container")
	tracker.Remove("abc123")

	assert.False(t, tracker.IsTracked("abc123"))
	assert.False(t, tracker.IsTracked("my-container"))
}

func TestTracker_IsTracked_ByID(t *testing.T) {
	tracker := NewTracker()
	tracker.Add("container-id-1", "name-1")

	assert.True(t, tracker.IsTracked("container-id-1"))
	assert.False(t, tracker.IsTracked("nonexistent-id"))
}

func TestTracker_IsTracked_ByName(t *testing.T) {
	tracker := NewTracker()
	tracker.Add("container-id-1", "my-app")

	assert.True(t, tracker.IsTracked("my-app"))
	assert.False(t, tracker.IsTracked("other-app"))
}

func TestTracker_IDByName(t *testing.T) {
	tracker := NewTracker()
	tracker.Add("full-id-abc123", "web-server")

	assert.Equal(t, "full-id-abc123", tracker.IDByName("web-server"))
	assert.Equal(t, "", tracker.IDByName("nonexistent"))
}

func TestTracker_ResolveID(t *testing.T) {
	tracker := NewTracker()
	tracker.Add("full-id-abc123", "web-server")

	t.Run("resolves by ID", func(t *testing.T) {
		assert.Equal(t, "full-id-abc123", tracker.ResolveID("full-id-abc123"))
	})

	t.Run("resolves by name", func(t *testing.T) {
		assert.Equal(t, "full-id-abc123", tracker.ResolveID("web-server"))
	})

	t.Run("returns empty for untracked", func(t *testing.T) {
		assert.Equal(t, "", tracker.ResolveID("unknown"))
	})
}

func TestTracker_List(t *testing.T) {
	tracker := NewTracker()

	t.Run("empty tracker", func(t *testing.T) {
		list := tracker.List()
		assert.Empty(t, list)
	})

	t.Run("with containers", func(t *testing.T) {
		tracker.Add("id-1", "name-1")
		tracker.Add("id-2", "name-2")

		list := tracker.List()
		require.Len(t, list, 2)

		// Sort for deterministic comparison
		sort.Slice(list, func(i, j int) bool {
			return list[i].ID < list[j].ID
		})

		assert.Equal(t, "id-1", list[0].ID)
		assert.Equal(t, "name-1", list[0].Name)
		assert.Equal(t, "id-2", list[1].ID)
		assert.Equal(t, "name-2", list[1].Name)
	})
}

func TestTracker_MultipleContainers(t *testing.T) {
	tracker := NewTracker()

	tracker.Add("id-1", "app-1")
	tracker.Add("id-2", "app-2")
	tracker.Add("id-3", "app-3")

	assert.True(t, tracker.IsTracked("id-1"))
	assert.True(t, tracker.IsTracked("app-2"))
	assert.True(t, tracker.IsTracked("id-3"))
	assert.False(t, tracker.IsTracked("id-4"))

	tracker.Remove("id-2")

	assert.True(t, tracker.IsTracked("id-1"))
	assert.False(t, tracker.IsTracked("id-2"))
	assert.False(t, tracker.IsTracked("app-2"))
	assert.True(t, tracker.IsTracked("id-3"))
}
