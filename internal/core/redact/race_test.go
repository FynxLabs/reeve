package redact

import (
	"fmt"
	"sync"
	"testing"
)

// TestRedactorConcurrentAddAndRedact exercises the drift-runner pattern:
// parallel goroutines register secrets while others redact. Run with -race;
// before the mutex this was a concurrent map read/write panic.
func TestRedactorConcurrentAddAndRedact(t *testing.T) {
	r := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			r.AddSecret(fmt.Sprintf("supersecret-value-%03d", n))
		}(i)
		go func(n int) {
			defer wg.Done()
			_ = r.Redact(fmt.Sprintf("log line with supersecret-value-%03d in it", n))
		}(i)
	}
	wg.Wait()
}
