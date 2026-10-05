// Package rediskeys builds every Redis key this service uses, so one Redis
// can host several deployments behind REDIS_KEY_PREFIX.
package rediskeys

import "fmt"

type Keys struct {
	prefix string
}

func New(prefix string) Keys {
	if prefix != "" {
		prefix += ":"
	}
	return Keys{prefix: prefix}
}

// RPM is the fixed-window request counter per client key and model.
func (k Keys) RPM(keyName, modelID string, unixMinute int64) string {
	return fmt.Sprintf("%sllmproxy:rpm:%s:%s:%d", k.prefix, keyName, modelID, unixMinute)
}

// BudgetKey is the daily token budget per client key and model (UTC day).
func (k Keys) BudgetKey(keyName, modelID, day string) string {
	return fmt.Sprintf("%sllmproxy:bud:k:%s:%s:%s", k.prefix, keyName, modelID, day)
}

// BudgetUser is the daily token budget per end user and model (UTC day).
func (k Keys) BudgetUser(userID, modelID, day string) string {
	return fmt.Sprintf("%sllmproxy:bud:u:%s:%s:%s", k.prefix, userID, modelID, day)
}
