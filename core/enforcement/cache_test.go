package enforcement

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCache_UpdateAndSnapshot(t *testing.T) {
	c := newCache()
	assert.Empty(t, c.snapshot())
	assert.True(t, c.lastUpdated().IsZero())

	rules := []Rule{
		{RuleID: 1, Action: ActionBlock, Threshold: 100, CurrentValue: 200, Breached: true},
		{RuleID: 2, Action: ActionWarnOnly, Threshold: 80, CurrentValue: 90, Breached: true},
	}
	c.update(rules)

	snap := c.snapshot()
	assert.Len(t, snap, 2)
	assert.Equal(t, int64(1), snap[0].RuleID)
	assert.False(t, c.lastUpdated().IsZero())
}

func TestCache_SnapshotIsCopy(t *testing.T) {
	c := newCache()
	c.update([]Rule{{RuleID: 1, Name: "orig"}})

	snap := c.snapshot()
	snap[0].Name = "mutated"

	snap2 := c.snapshot()
	assert.Equal(t, "orig", snap2[0].Name, "mutation of snapshot should not affect cache")
}
