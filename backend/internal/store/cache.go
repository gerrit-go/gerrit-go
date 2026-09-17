package store

import (
	"sync"
	"time"
)

// permCacheTTL bounds how long cached access rules and group memberships live
// as a safety net; writes invalidate entries eagerly, so the TTL only covers
// out-of-band changes (e.g. direct DB edits).
const permCacheTTL = 60 * time.Second

type rulesCacheEntry struct {
	rules   []*AccessRule
	expires time.Time
}

type groupsCacheEntry struct {
	groups  []*Group
	expires time.Time
}

// permCache caches the inherited access rules per project and the group
// memberships per account to keep permission checks off the database hot path.
// Writes invalidate the affected entries; a short TTL bounds staleness from
// any change that bypasses the store.
type permCache struct {
	mu     sync.RWMutex
	rules  map[string]rulesCacheEntry
	groups map[int64]groupsCacheEntry
}

func newPermCache() *permCache {
	return &permCache{
		rules:  map[string]rulesCacheEntry{},
		groups: map[int64]groupsCacheEntry{},
	}
}

func (c *permCache) getRules(project string) ([]*AccessRule, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.rules[project]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.rules, true
}

func (c *permCache) setRules(project string, rules []*AccessRule) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules[project] = rulesCacheEntry{rules: rules, expires: time.Now().Add(permCacheTTL)}
}

func (c *permCache) getGroups(accountID int64) ([]*Group, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.groups[accountID]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.groups, true
}

func (c *permCache) setGroups(accountID int64, groups []*Group) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.groups[accountID] = groupsCacheEntry{groups: groups, expires: time.Now().Add(permCacheTTL)}
}

// invalidateRules drops one project's cached rules (and, because children
// inherit, every project's rules — parent changes affect descendants, so the
// simplest correct invalidation is to clear the whole rules map).
func (c *permCache) invalidateRules() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules = map[string]rulesCacheEntry{}
}

// invalidateGroups drops one account's cached memberships.
func (c *permCache) invalidateGroups(accountID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.groups, accountID)
}

// invalidateAllGroups drops all cached memberships (e.g. a group was deleted).
func (c *permCache) invalidateAllGroups() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.groups = map[int64]groupsCacheEntry{}
}
