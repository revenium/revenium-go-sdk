package enforcement

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCache_UpdateAndSnapshot(t *testing.T) {
	c := newCache()
	assert.Empty(t, c.snapshot().rules)

	rules := []Rule{
		{RuleID: 1, Action: ActionBlock, Threshold: 100, CurrentValue: 200, Breached: true},
		{RuleID: 2, Action: ActionWarnOnly, Threshold: 80, CurrentValue: 90, Breached: true},
	}
	c.update(ruleSet{rules: rules})

	snap := c.snapshot().rules
	assert.Len(t, snap, 2)
	assert.Equal(t, int64(1), snap[0].RuleID)
}

func TestCache_SnapshotIsCopy(t *testing.T) {
	c := newCache()
	c.update(ruleSet{rules: []Rule{{RuleID: 1, Name: "orig"}}})

	snap := c.snapshot().rules
	snap[0].Name = "mutated"

	snap2 := c.snapshot().rules
	assert.Equal(t, "orig", snap2[0].Name, "mutation of snapshot should not affect cache")
}

func TestCache_StaleRulesFailOpen(t *testing.T) {
	c := newCache()
	c.update(ruleSet{rules: []Rule{{RuleID: 1, Action: ActionBlock, Breached: true}}})

	c.mu.Lock()
	c.updatedAt = time.Now().Add(-ruleStaleAfter - time.Second)
	c.mu.Unlock()

	assert.Empty(t, c.snapshot().rules, "rules past the staleness bound must not keep deciding calls")

	c.update(ruleSet{rules: []Rule{{RuleID: 1, Action: ActionBlock, Breached: true}}})
	assert.Len(t, c.snapshot().rules, 1, "a fresh update restores enforcement")
}
