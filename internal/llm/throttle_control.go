package llm

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ThrottleControl manages up to `limit` requests per `timeWindow` seconds.
// If you call WaitForSlot() (instead of CanMakeRequest()), it will block
// until a new request can be “taken” without exceeding the limit.
type ThrottleControl struct {
	limit        int           // max requests allowed in a rolling window
	timeWindow   time.Duration // rolling window duration (in seconds)
	requestTimes []int64       // Unix‐second timestamps of recent requests
	lock         sync.Mutex    // guards access to requestTimes
}

func NewThrottleControl(limit int, timeWindowSeconds int) *ThrottleControl {
	return &ThrottleControl{
		limit:        limit,
		timeWindow:   time.Duration(timeWindowSeconds) * time.Second,
		requestTimes: make([]int64, 0, limit),
	}
}

// WaitForSlot will block until you are allowed to record a new request.
// Once it returns, a new timestamp has already been taken for you.
func (tc *ThrottleControl) WaitForSlot() {
	if tc.limit <= 0 {
		// If limit is 0, we can always take a slot immediately
		return
	}
	for {
		tc.lock.Lock()
		now := time.Now().Unix()
		earliestAllowed := now - int64(tc.timeWindow.Seconds())

		// Remove old timestamps
		j := 0
		for j < len(tc.requestTimes) && tc.requestTimes[j] <= earliestAllowed {
			j++
		}
		tc.requestTimes = tc.requestTimes[j:]

		// If below limit, record and return immediately
		if len(tc.requestTimes) < tc.limit {
			tc.requestTimes = append(tc.requestTimes, now)
			tc.lock.Unlock()
			return
		}

		// Otherwise, compute when the oldest entry will “fall out” of the window
		oldest := tc.requestTimes[0]
		// next time we can take a slot is oldest + timeWindow
		target := time.Unix(oldest, 0).Add(tc.timeWindow)
		tc.lock.Unlock()

		sleepDuration := time.Until(target)
		if sleepDuration <= 0 {
			// it’s already expired; loop around without sleeping
			continue
		}
		log.Info().Str("wait_until", target.Format(time.RFC3339)).Dur("sleep_duration", sleepDuration).Msg("ThrottleControl waiting for slot")
		time.Sleep(sleepDuration)
	}
}
