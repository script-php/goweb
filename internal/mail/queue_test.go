package mail

import (
	"testing"
	"time"
)

func TestRetryDelay(t *testing.T) {
	wants := []time.Duration{time.Minute, time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour}
	for attempt, want := range wants {
		if got := retryDelay(attempt); got != want {
			t.Errorf("retryDelay(%d) = %s, want %s", attempt, got, want)
		}
	}
}
