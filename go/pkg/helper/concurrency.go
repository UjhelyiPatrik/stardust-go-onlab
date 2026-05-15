package helper

import (
	"runtime"
	"sync"
)

// ParallelFor is a helper function that executes a given task concurrently for each item in the provided slice.
func ParallelFor[T any](items []T, task func(T)) {
	if len(items) == 0 {
		return
	}

	// Get the number of available CPU cores
	numWorkers := runtime.GOMAXPROCS(0)
	if len(items) < numWorkers {
		numWorkers = len(items)
	}

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	chunkSize := (len(items) + numWorkers - 1) / numWorkers

	for i := 0; i < numWorkers; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(items) {
			end = len(items)
		}

		// Launch a goroutine for each chunk of items
		go func(subItems []T) {
			defer wg.Done()
			for _, item := range subItems {
				task(item)
			}
		}(items[start:end])
	}
	wg.Wait()
}
