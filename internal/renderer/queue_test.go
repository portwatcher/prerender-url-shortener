package renderer

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"prerender-url-shortener/internal/db"

	"github.com/jinzhu/gorm"
	"github.com/stretchr/testify/assert"
)

// Mock database functions for testing
var mockDBStatus = make(map[string]db.RenderStatus)

// mockDB implements DBInterface for testing
type mockDB struct{}

func (m *mockDB) GetLinkByShortCode(shortCode string) (*db.Link, error) {
	if status, exists := mockDBStatus[shortCode]; exists {
		return &db.Link{
			ShortCode:    shortCode,
			OriginalURL:  "https://example.com", // Not used in tests
			RenderStatus: status,
		}, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *mockDB) UpdateLinkRenderStatus(shortCode string, status db.RenderStatus) error {
	mockDBStatus[shortCode] = status
	return nil
}

func (m *mockDB) UpdateLinkContent(shortCode string, content string, status db.RenderStatus) error {
	mockDBStatus[shortCode] = status
	return nil
}

// Reset mock database state
func resetMockDB() {
	mockDBStatus = make(map[string]db.RenderStatus)
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
				workerCount: tt.workerCount,
				db:          &mockDB{}, // Add mock database
			}

			// Start workers (without using the global variable)
			for i := 0; i < tt.workerCount; i++ {
				go queue.worker(i)
			}

			assert.Equal(t, tt.workerCount, queue.workerCount)
			assert.NotNil(t, queue.jobs)
			assert.NotNil(t, queue.inProgress)

			// Clean up
			close(queue.jobs)
		})
	}
}

func TestQueueRender(t *testing.T) {
	queue := &RenderQueue{
		jobs:        make(chan RenderJob, 10),
		inProgress:  make(map[string]bool),
		workerCount: 1,
		db:          &mockDB{}, // Add mock database
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
				// Set database status to rendering to prevent re-queuing
				mockDBStatus["DEF456"] = db.RenderStatusRendering
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock DB state
			resetMockDB()

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
		workerCount: 1,
		db:          &mockDB{}, // Add mock database
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

func TestGetStatus(t *testing.T) {
	queue := &RenderQueue{
		jobs:        make(chan RenderJob, 10),
		inProgress:  make(map[string]bool),
		workerCount: 3,
		db:          &mockDB{}, // Add mock database
	}

	// Add some test data
	queue.jobs <- RenderJob{ShortCode: "ABC", OriginalURL: "https://example1.com"}
	queue.jobs <- RenderJob{ShortCode: "DEF", OriginalURL: "https://example2.com"}

	queue.inProgress["https://inprogress1.com"] = true
	queue.inProgress["https://inprogress2.com"] = true

	status := queue.GetStatus()

	assert.Equal(t, 3, status["worker_count"])
	assert.Equal(t, 2, status["queue_length"])
	assert.Equal(t, 2, status["in_progress_count"])

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
		workerCount: 5,
		db:          &mockDB{}, // Add mock database
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
		workerCount: 1,
		db:          &mockDB{}, // Add mock database
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

func TestQueueRenderDuplicateURLs(t *testing.T) {
	// Reset mock DB state
	resetMockDB()

	queue := &RenderQueue{
		jobs:        make(chan RenderJob, 10),
		inProgress:  make(map[string]bool),
		workerCount: 1,
		db:          &mockDB{}, // Add mock database
	}

	// Queue the same URL multiple times
	url := "https://duplicate.com"
	queue.QueueRender("CODE1", url)

	// Set database status to rendering to prevent re-queuing
	mockDBStatus["CODE2"] = db.RenderStatusRendering
	queue.QueueRender("CODE2", url) // Should be skipped

	// Set database status to rendering to prevent re-queuing
	mockDBStatus["CODE3"] = db.RenderStatusRendering
	queue.QueueRender("CODE3", url) // Should be skipped

	// Only one job should be queued
	assert.Equal(t, 1, len(queue.jobs))
	assert.True(t, queue.IsInProgress(url))

	// Clean up
	close(queue.jobs)
}

func BenchmarkQueueRender(b *testing.B) {
	queue := &RenderQueue{
		jobs:        make(chan RenderJob, 1000),
		inProgress:  make(map[string]bool),
		workerCount: 1,
		db:          &mockDB{}, // Add mock database
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
		workerCount: 1,
		db:          &mockDB{}, // Add mock database
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

func TestQueueRenderWithDatabaseStatus(t *testing.T) {
	tests := []struct {
		name           string
		shortCode      string
		originalURL    string
		inProgress     bool
		dbStatus       db.RenderStatus
		shouldQueue    bool
		expectedStatus db.RenderStatus
	}{
		{
			name:           "new URL not in progress",
			shortCode:      "NEW1",
			originalURL:    "https://new1.com",
			inProgress:     false,
			dbStatus:       db.RenderStatusPending,
			shouldQueue:    true,
			expectedStatus: db.RenderStatusPending,
		},
		{
			name:           "URL in progress with pending status",
			shortCode:      "PENDING1",
			originalURL:    "https://pending1.com",
			inProgress:     true,
			dbStatus:       db.RenderStatusPending,
			shouldQueue:    true, // Should re-queue because DB status is pending
			expectedStatus: db.RenderStatusPending,
		},
		{
			name:           "URL in progress with rendering status",
			shortCode:      "RENDERING1",
			originalURL:    "https://rendering1.com",
			inProgress:     true,
			dbStatus:       db.RenderStatusRendering,
			shouldQueue:    false, // Should not queue because DB status is rendering
			expectedStatus: db.RenderStatusRendering,
		},
		{
			name:           "URL in progress with completed status",
			shortCode:      "COMPLETE1",
			originalURL:    "https://complete1.com",
			inProgress:     true,
			dbStatus:       db.RenderStatusCompleted,
			shouldQueue:    false, // Should not queue because DB status is completed
			expectedStatus: db.RenderStatusCompleted,
		},
		{
			name:           "URL in progress with failed status",
			shortCode:      "FAILED1",
			originalURL:    "https://failed1.com",
			inProgress:     true,
			dbStatus:       db.RenderStatusFailed,
			shouldQueue:    false, // Should not queue because DB status is failed
			expectedStatus: db.RenderStatusFailed,
		},
		{
			name:           "URL in progress but not in database",
			shortCode:      "MISSING1",
			originalURL:    "https://missing1.com",
			inProgress:     true,
			dbStatus:       "",    // Not set in mock DB
			shouldQueue:    false, // Should not queue because DB lookup failed
			expectedStatus: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock DB state
			resetMockDB()

			// Set up initial state
			queue := &RenderQueue{
				jobs:        make(chan RenderJob, 10),
				inProgress:  make(map[string]bool),
				workerCount: 1,
				db:          &mockDB{}, // Inject mock DB
			}

			if tt.inProgress {
				queue.inProgress[tt.originalURL] = true
			}

			if tt.dbStatus != "" {
				mockDBStatus[tt.shortCode] = tt.dbStatus
			}

			// Record initial queue length
			initialQueueLength := len(queue.jobs)

			// Attempt to queue the job
			queue.QueueRender(tt.shortCode, tt.originalURL)

			// Verify queue length
			if tt.shouldQueue {
				assert.Equal(t, initialQueueLength+1, len(queue.jobs), "Queue length should have increased")
				assert.True(t, queue.inProgress[tt.originalURL], "URL should be marked as in progress")
			} else {
				assert.Equal(t, initialQueueLength, len(queue.jobs), "Queue length should not have changed")
				if tt.dbStatus == db.RenderStatusPending {
					assert.False(t, queue.inProgress[tt.originalURL], "URL should not be marked as in progress for pending status")
				} else {
					assert.Equal(t, tt.inProgress, queue.inProgress[tt.originalURL], "In-progress status should remain unchanged")
				}
			}

			// Verify database status if it was set
			if tt.dbStatus != "" {
				assert.Equal(t, tt.expectedStatus, mockDBStatus[tt.shortCode], "Database status should match expected")
			}
		})
	}
}
