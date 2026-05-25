package main

import "sync"

// TrackedContainer holds information about a container tracked by this session.
type TrackedContainer struct {
	ID   string
	Name string
}

// Tracker keeps track of containers created by this MCP session.
type Tracker struct {
	containers sync.Map // key: container ID, value: container name
}

// NewTracker creates a new container tracker.
func NewTracker() *Tracker {
	return &Tracker{}
}

// Add registers a container as tracked.
func (t *Tracker) Add(id, name string) {
	t.containers.Store(id, name)
}

// Remove unregisters a container from tracking.
func (t *Tracker) Remove(id string) {
	t.containers.Delete(id)
}

// IsTracked checks whether a container (by ID or name) is tracked.
func (t *Tracker) IsTracked(idOrName string) bool {
	// Check by ID
	if _, ok := t.containers.Load(idOrName); ok {
		return true
	}
	// Check by name
	found := false
	t.containers.Range(func(key, value any) bool {
		if value.(string) == idOrName {
			found = true
			return false
		}
		return true
	})
	return found
}

// IDByName returns the container ID for a given name, or empty string if not found.
func (t *Tracker) IDByName(name string) string {
	var id string
	t.containers.Range(func(key, value any) bool {
		if value.(string) == name {
			id = key.(string)
			return false
		}
		return true
	})
	return id
}

// ResolveID resolves an ID or name to a container ID. Returns empty string if not tracked.
func (t *Tracker) ResolveID(idOrName string) string {
	// Check if it's directly a tracked ID
	if _, ok := t.containers.Load(idOrName); ok {
		return idOrName
	}
	// Check if it's a name
	return t.IDByName(idOrName)
}

// List returns all tracked containers.
func (t *Tracker) List() []TrackedContainer {
	var result []TrackedContainer
	t.containers.Range(func(key, value any) bool {
		result = append(result, TrackedContainer{
			ID:   key.(string),
			Name: value.(string),
		})
		return true
	})
	return result
}
