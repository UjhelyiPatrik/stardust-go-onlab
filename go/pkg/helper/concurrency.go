package helper

import (
	"runtime"
	"sync"
)

// ParallelFor is a helper function that executes a given task concurrently for each item in the provided slice.
func ParallelFor[T any](items []T, task func(T)) {
	n := len(items)
	if n == 0 {
		return
	}

	// Get the number of available CPU cores
	numWorkers := runtime.GOMAXPROCS(0)
	if n < numWorkers {
		numWorkers = n
	}

	chunkSize := (n + numWorkers - 1) / numWorkers

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		start := i * chunkSize
		if start >= n {
			break
		}
		end := start + chunkSize
		if end > n {
			end = n
		}

		wg.Add(1)
		go func(subItems []T) {
			defer wg.Done()
			for _, item := range subItems {
				task(item)
			}
		}(items[start:end])
	}
	wg.Wait()
}
