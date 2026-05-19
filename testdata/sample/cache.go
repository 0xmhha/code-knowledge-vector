package sample

import "sync"

// Cache is a simple thread-safe in-memory key/value store, kept here
// as a second source file so the indexer covers the multi-file case.
type Cache struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewCache returns an empty cache.
func NewCache() *Cache {
	return &Cache{data: make(map[string]string)}
}

// Set stores value under key.
func (c *Cache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = value
}

// Get retrieves the value for key, returning ok=false if absent.
func (c *Cache) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[key]
	return v, ok
}

// Delete removes key from the cache. No-op if key is absent.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}

// Len returns the current number of entries in the cache.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// EvictAll triggers eviction of every entry currently in the cache.
// Used by the Server.Close path to drop in-memory state when shutting
// the network listener down.
func (c *Cache) EvictAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.data {
		delete(c.data, k)
	}
}
