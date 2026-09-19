package jira

import "slices"

// Clone returns a configuration snapshot whose slices can be edited independently.
func (c Config) Clone() Config {
	c.Instances = slices.Clone(c.Instances)
	c.PinnedKeys = slices.Clone(c.PinnedKeys)
	return c
}
