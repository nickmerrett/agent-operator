/*
Copyright 2024 Nick Merrett.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tools

import (
	"sync"
)

// Catalogue is a thread-safe registry of all available tools.
// Built-in tools are registered at operator startup. MCPServer tools are added
// dynamically as MCPServer CRs are reconciled.
type Catalogue struct {
	mu      sync.RWMutex
	entries map[string]Entry
}

// globalCatalogue is the process-wide tool registry.
var globalCatalogue = &Catalogue{
	entries: make(map[string]Entry),
}

// Global returns the process-wide tool catalogue.
func Global() *Catalogue {
	return globalCatalogue
}

// Register adds a tool to the catalogue. Overwrites any existing entry with the same name.
func (c *Catalogue) Register(e Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[e.Spec.Name] = e
}

// Get retrieves a single tool by name. Returns false if not found.
func (c *Catalogue) Get(name string) (Entry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[name]
	return e, ok
}

// List returns all registered tool specs (no executors).
func (c *Catalogue) List() []Spec {
	c.mu.RLock()
	defer c.mu.RUnlock()
	specs := make([]Spec, 0, len(c.entries))
	for _, e := range c.entries {
		specs = append(specs, e.Spec)
	}
	return specs
}

// Find returns all tools whose name or description contains the query string (case-insensitive).
func (c *Catalogue) Find(query string) []Spec {
	c.mu.RLock()
	defer c.mu.RUnlock()

	query = lowercase(query)
	var results []Spec
	for _, e := range c.entries {
		if contains(lowercase(e.Spec.Name), query) || contains(lowercase(e.Spec.Description), query) {
			results = append(results, e.Spec)
		}
	}
	return results
}


func lowercase(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		result[i] = c
	}
	return string(result)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
