package renderer

import (
	"log"
	"prerender-url-shortener/internal/db"
	"sync"
	"time"
)

// RenderJob represents a rendering job in the queue
type RenderJob struct {
	ShortCode   string
	OriginalURL string
}

// RenderQueue manages the rendering queue and prevents duplicate work
type RenderQueue struct {
	jobs        chan RenderJob
	inProgress  map[string]bool        // Track URLs currently being rendered
	waiting     map[string][]chan bool // Track goroutines waiting for specific URLs
	mutex       sync.RWMutex
	workerCount int
}

var GlobalRenderQueue *RenderQueue

// InitRenderQueue initializes the global render queue
func InitRenderQueue(workerCount int) {
	GlobalRenderQueue = &RenderQueue{
		jobs:        make(chan RenderJob, 100), // Buffer for 100 jobs
		inProgress:  make(map[string]bool),
		waiting:     make(map[string][]chan bool),
		workerCount: workerCount,
	}

	// Start worker goroutines
	for i := 0; i < workerCount; i++ {
		go GlobalRenderQueue.worker(i)
	}

	log.Printf("Initialized render queue with %d workers", workerCount)
}

// QueueRender adds a job to the rendering queue or waits if already in progress
func (rq *RenderQueue) QueueRender(shortCode, originalURL string) {
	rq.mutex.Lock()
	defer rq.mutex.Unlock()

	log.Printf("Queue: Attempting to queue render job for URL: %s (short code: %s)", originalURL, shortCode)

	// Check if this URL is already being rendered
	if rq.inProgress[originalURL] {
		log.Printf("Queue: URL %s is already being rendered, not queuing duplicate", originalURL)
		return
	}

	// Mark as in progress and queue the job
	rq.inProgress[originalURL] = true

	queueLength := len(rq.jobs)
	log.Printf("Queue: Current queue length: %d before adding new job", queueLength)

	select {
	case rq.jobs <- RenderJob{ShortCode: shortCode, OriginalURL: originalURL}:
		log.Printf("Queue: Successfully queued rendering job for URL: %s (short code: %s)", originalURL, shortCode)
	default:
		log.Printf("Queue: Render queue is full (capacity: 100), dropping job for URL: %s", originalURL)
		// Clean up in-progress status if we can't queue
		delete(rq.inProgress, originalURL)
	}
}

// WaitForRender waits for a URL to be rendered if it's already in progress
func (rq *RenderQueue) WaitForRender(originalURL string, timeout time.Duration) bool {
	log.Printf("Queue: Checking if should wait for URL: %s (timeout: %v)", originalURL, timeout)

	rq.mutex.Lock()

	// If not in progress, return immediately
	if !rq.inProgress[originalURL] {
		rq.mutex.Unlock()
		log.Printf("Queue: URL %s is not in progress, no need to wait", originalURL)
		return false
	}

	// Create a channel to wait on
	waitChan := make(chan bool, 1)
	rq.waiting[originalURL] = append(rq.waiting[originalURL], waitChan)
	currentWaiters := len(rq.waiting[originalURL])
	rq.mutex.Unlock()

	log.Printf("Queue: Added to waiting list for URL %s (total waiters: %d), starting wait...", originalURL, currentWaiters)

	// Wait for completion or timeout
	select {
	case <-waitChan:
		log.Printf("Queue: Wait completed successfully for URL: %s", originalURL)
		return true
	case <-time.After(timeout):
		log.Printf("Queue: Wait timeout after %v for URL: %s, cleaning up", timeout, originalURL)
		// Remove ourselves from the waiting list
		rq.mutex.Lock()
		waiters := rq.waiting[originalURL]
		for i, ch := range waiters {
			if ch == waitChan {
				rq.waiting[originalURL] = append(waiters[:i], waiters[i+1:]...)
				log.Printf("Queue: Removed timed-out waiter from list for URL: %s", originalURL)
				break
			}
		}
		rq.mutex.Unlock()
		return false
	}
}

// worker processes rendering jobs
func (rq *RenderQueue) worker(id int) {
	log.Printf("Render worker %d started", id)

	for job := range rq.jobs {
		startTime := time.Now()
		log.Printf("Worker %d: Starting job for URL: %s (short code: %s)", id, job.OriginalURL, job.ShortCode)

		// Update status to rendering
		log.Printf("Worker %d: Updating database status to 'rendering' for %s", id, job.ShortCode)
		if err := db.UpdateLinkRenderStatus(job.ShortCode, db.RenderStatusRendering); err != nil {
			log.Printf("Worker %d: Failed to update status to rendering for %s: %v", id, job.ShortCode, err)
		} else {
			log.Printf("Worker %d: Successfully updated status to 'rendering' for %s", id, job.ShortCode)
		}

		// Perform the actual rendering
		log.Printf("Worker %d: Starting Rod rendering for URL: %s", id, job.OriginalURL)
		renderStartTime := time.Now()
		htmlContent, err := RenderPageWithRod(job.OriginalURL)
		renderDuration := time.Since(renderStartTime)

		rq.mutex.Lock()

		if err != nil {
			log.Printf("Worker %d: Failed to render %s after %v: %v", id, job.OriginalURL, renderDuration, err)
			// Update status to failed
			log.Printf("Worker %d: Updating database status to 'failed' for %s", id, job.ShortCode)
			if dbErr := db.UpdateLinkContent(job.ShortCode, "", db.RenderStatusFailed); dbErr != nil {
				log.Printf("Worker %d: Failed to update status to failed for %s: %v", id, job.ShortCode, dbErr)
			} else {
				log.Printf("Worker %d: Successfully updated status to 'failed' for %s", id, job.ShortCode)
			}
		} else {
			log.Printf("Worker %d: Successfully rendered %s in %v (HTML length: %d)", id, job.OriginalURL, renderDuration, len(htmlContent))
			// Update with rendered content
			log.Printf("Worker %d: Saving rendered content to database for %s", id, job.ShortCode)
			if dbErr := db.UpdateLinkContent(job.ShortCode, htmlContent, db.RenderStatusCompleted); dbErr != nil {
				log.Printf("Worker %d: Failed to save rendered content for %s: %v", id, job.ShortCode, dbErr)
			} else {
				log.Printf("Worker %d: Successfully saved rendered content for %s", id, job.ShortCode)
			}
		}

		// Notify waiting goroutines
		waiters := rq.waiting[job.OriginalURL]
		if len(waiters) > 0 {
			log.Printf("Worker %d: Notifying %d waiting goroutines for URL %s", id, len(waiters), job.OriginalURL)
			for i, waitChan := range waiters {
				select {
				case waitChan <- true:
					log.Printf("Worker %d: Notified waiter %d for URL %s", id, i+1, job.OriginalURL)
				default:
					log.Printf("Worker %d: Failed to notify waiter %d for URL %s (channel full)", id, i+1, job.OriginalURL)
				}
			}
		}
		delete(rq.waiting, job.OriginalURL)

		// Mark as no longer in progress
		delete(rq.inProgress, job.OriginalURL)
		log.Printf("Worker %d: Marked URL %s as no longer in progress", id, job.OriginalURL)

		rq.mutex.Unlock()

		totalDuration := time.Since(startTime)
		log.Printf("Worker %d: Completed job for %s in %v (render: %v, total: %v)", id, job.OriginalURL, totalDuration, renderDuration, totalDuration)
	}

	log.Printf("Render worker %d stopped (jobs channel closed)", id)
}

// IsInProgress checks if a URL is currently being rendered
func (rq *RenderQueue) IsInProgress(originalURL string) bool {
	rq.mutex.RLock()
	defer rq.mutex.RUnlock()
	return rq.inProgress[originalURL]
}

// GetStatus returns the current status of the render queue
func (rq *RenderQueue) GetStatus() map[string]interface{} {
	rq.mutex.RLock()
	defer rq.mutex.RUnlock()

	inProgressURLs := make([]string, 0, len(rq.inProgress))
	for url := range rq.inProgress {
		inProgressURLs = append(inProgressURLs, url)
	}

	waitingCount := 0
	for _, waiters := range rq.waiting {
		waitingCount += len(waiters)
	}

	return map[string]interface{}{
		"worker_count":       rq.workerCount,
		"queue_length":       len(rq.jobs),
		"in_progress_count":  len(rq.inProgress),
		"in_progress_urls":   inProgressURLs,
		"waiting_goroutines": waitingCount,
	}
}

// Shutdown gracefully shuts down the render queue
func (rq *RenderQueue) Shutdown() {
	close(rq.jobs)
	log.Println("Render queue shutdown initiated")
}
