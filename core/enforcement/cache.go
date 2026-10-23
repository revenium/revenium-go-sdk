package enforcement

import (
	"sync"
	"time"
)

// ruleStaleAfter bounds how long a loaded rule set keeps deciding calls
// after the endpoint stops answering. Past it the engine fails open, like
// an engine that never fetched, so a stale BLOCK (for example a budget the
// operator has since raised) cannot outlive the outage indefinitely. Ten
// missed polls is well past any transient blip.
const ruleStaleAfter = 10 * pollInterval

// cache is a thread-safe in-memory store for one team's rule set.
type cache struct {
	mu        sync.RWMutex
	set       ruleSet
	updatedAt time.Time
}

func newCache() *cache {
	return &cache{}
}

// update replaces the entire rule set atomically.
func (c *cache) update(set ruleSet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.set = set
	c.updatedAt = time.Now()
}

// snapshot returns a copy of the current rule set so callers can evaluate
// without holding the lock. The department maps are replaced, never
// mutated, so sharing them is safe.
func (c *cache) snapshot() ruleSet {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.updatedAt.IsZero() || time.Since(c.updatedAt) > ruleStaleAfter {
		return ruleSet{}
	}
	out := c.set
	out.rules = make([]Rule, len(c.set.rules))
	copy(out.rules, c.set.rules)
	return out
}
