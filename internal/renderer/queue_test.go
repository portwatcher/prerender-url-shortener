package renderer

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Mock renderer function for testing
func mockRenderPageWithRod(url string) (string, error) {
	// Simulate some work
	time.Sleep(10 * time.Millisecond)
	return "<html><body>Mock content for " + url + "</body></html>", nil
}

func TestInitRenderQueue(t *testing.T) {
	tests := []struct {
		name        string
		workerCount int
	}{
		{"single worker", 1},
		{"multiple workers", 3},
		{"many workers", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new queue for each test
			queue := &RenderQueue{
				jobs:        make(chan RenderJob, 100),
				inProgress:  make(map[string]bool),
				waiting:     make(map[string][]chan bool),
				workerCount: tt.workerCount,
			}

			// Start workers (without using the global variable)
			for i := 0; i < tt.workerCount; i++ {
				go queue.worker(i)
			}

			assert.Equal(t, tt.workerCount, queue.workerCount)
			assert.NotNil(t, queue.jobs)
			assert.NotNil(t, queue.inProgress)
			assert.NotNil(t, queue.waiting)

			// Clean up
			close(queue.jobs)
		})
	}
}

func TestQueueRender(t *testing.T) {
	queue := &RenderQueue{
		jobs:        make(chan RenderJob, 10),
		inProgress:  make(map[string]bool),
		waiting:     make(map[string][]chan bool),
		workerCount: 1,
	}

	tests := []struct {
		name        string
		shortCode   string
		originalURL string
		shouldQueue bool
		setup       func()
	}{
		{
			name:        "queue new job",
			shortCode:   "ABC123",
			originalURL: "https://example.com",
			shouldQueue: true,
			setup:       func() {},
		},
		{
			name:        "skip duplicate URL",
			shortCode:   "DEF456",
			originalURL: "https://example.com", // Same URL as above
			shouldQueue: false,
			setup: func() {
				queue.inProgress["https://example.com"] = true
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()

			initialQueueLength := len(queue.jobs)
			queue.QueueRender(tt.shortCode, tt.originalURL)

			if tt.shouldQueue {
				assert.Equal(t, initialQueueLength+1, len(queue.jobs))
				assert.True(t, queue.inProgress[tt.originalURL])
			} else {
				assert.Equal(t, initialQueueLength, len(queue.jobs))
			}
		})
	}

	// Clean up
	close(queue.jobs)
}

func TestIsInProgress(t *testing.T) {
	queue := &RenderQueue{
		jobs:        make(chan RenderJob, 10),
		inProgress:  make(map[string]bool),
		waiting:     make(map[string][]chan bool),
		workerCount: 1,
	}

	testURL := "https://test.com"

	// Initially not in progress
	assert.False(t, queue.IsInProgress(testURL))

	// Mark as in progress
	queue.mutex.Lock()
	queue.inProgress[testURL] = true
	queue.mutex.Unlock()

	assert.True(t, queue.IsInProgress(testURL))

	// Remove from progress
	queue.mutex.Lock()
	delete(queue.inProgress, testURL)
	queue.mutex.Unlock()

	assert.False(t, queue.IsInProgress(testURL))
}

func TestWaitForRender(t *testing.T) {
	queue := &RenderQueue{
		jobs:        make(chan RenderJob, 10),
		inProgress:  make(map[string]bool),
		waiting:     make(map[string][]chan bool),
		workerCount: 1,
	}

	testURL := "https://waittest.com"

	t.Run("not in progress", func(t *testing.T) {
		result := queue.WaitForRender(testURL, 100*time.Millisecond)
		assert.False(t, result)
	})

	t.Run("timeout while waiting", func(t *testing.T) {
		// Mark as in progress
		queue.mutex.Lock()
		queue.inProgress[testURL] = true
		queue.mutex.Unlock()

		start := time.Now()
		result := queue.WaitForRender(testURL, 50*time.Millisecond)
		elapsed := time.Since(start)

		assert.False(t, result)
		assert.True(t, elapsed >= 50*time.Millisecond)
		assert.True(t, elapsed < 100*time.Millisecond)
	})

	t.Run("wait completes successfully", func(t *testing.T) {
		testURL2 := "https://waittest2.com"

		// Mark as in progress
		queue.mutex.Lock()
		queue.inProgress[testURL2] = true
		queue.mutex.Unlock()

		// Start waiting in a goroutine
		var wg sync.WaitGroup
		var result bool
		wg.Add(1)
		go func() {
			defer wg.Done()
			result = queue.WaitForRender(testURL2, 1*time.Second)
		}()

		// Wait a bit, then simulate completion
		time.Sleep(10 * time.Millisecond)
		queue.mutex.Lock()
		waiters := queue.waiting[testURL2]
		if len(waiters) > 0 {
			for _, waiter := range waiters {
				waiter <- true
			}
			delete(queue.waiting, testURL2)
		}
		delete(queue.inProgress, testURL2)
		queue.mutex.Unlock()

		wg.Wait()
		assert.True(t, result)
	})
}

func TestGetStatus(t *testing.T) {
	queue := &RenderQueue{
		jobs:        make(chan RenderJob, 10),
		inProgress:  make(map[string]bool),
		waiting:     make(map[string][]chan bool),
		workerCount: 3,
	}

	// Add some test data
	queue.jobs <- RenderJob{ShortCode: "ABC", OriginalURL: "https://example1.com"}
	queue.jobs <- RenderJob{ShortCode: "DEF", OriginalURL: "https://example2.com"}

	queue.inProgress["https://inprogress1.com"] = true
	queue.inProgress["https://inprogress2.com"] = true

	queue.waiting["https://waiting.com"] = make([]chan bool, 2)

	status := queue.GetStatus()

	assert.Equal(t, 3, status["worker_count"])
	assert.Equal(t, 2, status["queue_length"])
	assert.Equal(t, 2, status["in_progress_count"])
	assert.Equal(t, 2, status["waiting_goroutines"])

	inProgressURLs, ok := status["in_progress_urls"].([]string)
	assert.True(t, ok)
	assert.Len(t, inProgressURLs, 2)
	assert.Contains(t, inProgressURLs, "https://inprogress1.com")
	assert.Contains(t, inProgressURLs, "https://inprogress2.com")

	// Clean up
	close(queue.jobs)
}

func TestRenderJob(t *testing.T) {
	job := RenderJob{
		ShortCode:   "TEST123",
		OriginalURL: "https://test.example.com",
	}

	assert.Equal(t, "TEST123", job.ShortCode)
	assert.Equal(t, "https://test.example.com", job.OriginalURL)
}

func TestConcurrentQueueOperations(t *testing.T) {
	queue := &RenderQueue{
		jobs:        make(chan RenderJob, 100),
		inProgress:  make(map[string]bool),
		waiting:     make(map[string][]chan bool),
		workerCount: 5,
	}

	const numGoroutines = 10
	const operationsPerGoroutine = 20

	var wg sync.WaitGroup

	// Start multiple goroutines performing queue operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				shortCode := fmt.Sprintf("CODE%d_%d", id, j)
				url := fmt.Sprintf("https://example%d_%d.com", id, j)

				queue.QueueRender(shortCode, url)
				queue.IsInProgress(url)

				// Simulate some work
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Verify that operations completed without race conditions
	assert.True(t, len(queue.jobs) <= numGoroutines*operationsPerGoroutine)
	assert.True(t, len(queue.inProgress) <= numGoroutines*operationsPerGoroutine)

	// Clean up
	close(queue.jobs)
}

func TestQueueCapacity(t *testing.T) {
	// Create queue with small capacity
	queue := &RenderQueue{
		jobs:        make(chan RenderJob, 2), // Small capacity
		inProgress:  make(map[string]bool),
		waiting:     make(map[string][]chan bool),
		workerCount: 1,
	}

	// Fill the queue
	queue.QueueRender("CODE1", "https://example1.com")
	queue.QueueRender("CODE2", "https://example2.com")

	assert.Equal(t, 2, len(queue.jobs))
	assert.Equal(t, 2, len(queue.inProgress))

	// Try to add one more (should be dropped)
	queue.QueueRender("CODE3", "https://example3.com")

	// Queue should still be full, but the URL shouldn't be marked as in progress
	assert.Equal(t, 2, len(queue.jobs))
	assert.False(t, queue.inProgress["https://example3.com"])

	// Clean up
	close(queue.jobs)
}

func BenchmarkQueueRender(b *testing.B) {
	queue := &RenderQueue{
		jobs:        make(chan RenderJob, 1000),
		inProgress:  make(map[string]bool),
		waiting:     make(map[string][]chan bool),
		workerCount: 1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		shortCode := fmt.Sprintf("BENCH%d", i)
		url := fmt.Sprintf("https://bench%d.com", i)
		queue.QueueRender(shortCode, url)
	}

	close(queue.jobs)
}

func BenchmarkIsInProgress(b *testing.B) {
	queue := &RenderQueue{
		jobs:        make(chan RenderJob, 100),
		inProgress:  make(map[string]bool),
		waiting:     make(map[string][]chan bool),
		workerCount: 1,
	}

	// Add some URLs to the in-progress map
	for i := 0; i < 100; i++ {
		queue.inProgress[fmt.Sprintf("https://bench%d.com", i)] = true
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		url := fmt.Sprintf("https://bench%d.com", i%100)
		queue.IsInProgress(url)
	}
}
