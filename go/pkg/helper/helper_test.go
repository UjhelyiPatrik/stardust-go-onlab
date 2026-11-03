package helper_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/polaris-slo-cloud/stardust-go/pkg/helper"
)

func TestParallelFor(t *testing.T) {
	t.Run("Empty slice does not panic", func(t *testing.T) {
		var empty []int
		helper.ParallelFor(empty, func(x int) {
			t.Errorf("should not be called")
		})
	})

	t.Run("Executes across all items and computes exact sum", func(t *testing.T) {
		n := 1000
		items := make([]int64, n)
		var expectedSum int64 = 0
		for i := 0; i < n; i++ {
			items[i] = int64(i + 1)
			expectedSum += items[i]
		}

		var actualSum int64 = 0
		helper.ParallelFor(items, func(x int64) {
			atomic.AddInt64(&actualSum, x)
		})

		if actualSum != expectedSum {
			t.Errorf("expected sum %d, got %d", expectedSum, actualSum)
		}
	})
}

func TestManualResetEvent(t *testing.T) {
	t.Run("Starts unset, unblocks on Set", func(t *testing.T) {
		mre := helper.NewManualResetEvent(false)

		done := make(chan bool)
		go func() {
			mre.Wait()
			done <- true
		}()

		select {
		case <-done:
			t.Fatalf("Wait() should have blocked on unset event")
		case <-time.After(50 * time.Millisecond):
			// Expected to block
		}

		mre.Set()

		select {
		case <-done:
			// Unblocked successfully
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("Wait() failed to unblock after Set()")
		}
	})

	t.Run("Reset causes Wait to block again", func(t *testing.T) {
		mre := helper.NewManualResetEvent(true)
		// Should return immediately since initial state is true
		mre.Wait()

		mre.Reset()

		done := make(chan bool)
		go func() {
			mre.Wait()
			done <- true
		}()

		select {
		case <-done:
			t.Fatalf("Wait() should block after Reset()")
		case <-time.After(50 * time.Millisecond):
			// Expected
		}

		mre.Set()
		select {
		case <-done:
			// OK
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("Wait() failed to unblock after second Set()")
		}
	})
}
