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
// Uses URL as the primary key for tracking tasks
type RenderQueue struct {
	jobs        chan RenderJob
	inProgress  map[string]bool // Track URLs currently being rendered (URL as key)
	mutex       sync.RWMutex
	workerCount int
}

var GlobalRenderQueue *RenderQueue

// InitRenderQueue initializes the global render queue
func InitRenderQueue(workerCount int) {
	GlobalRenderQueue = &RenderQueue{
		jobs:        make(chan RenderJob, 100), // Buffer for 100 jobs
		inProgress:  make(map[string]bool),
		workerCount: workerCount,
	}

	// Start worker goroutines
	for i := 0; i < workerCount; i++ {
		go GlobalRenderQueue.worker(i)
	}

	log.Printf("Initialized render queue with %d workers", workerCount)
}

// QueueRender adds a job to the rendering queue if not already in progress
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

// IsInProgress checks if a URL is currently being rendered
func (rq *RenderQueue) IsInProgress(originalURL string) bool {
	rq.mutex.RLock()
	defer rq.mutex.RUnlock()
	return rq.inProgress[originalURL]
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

		// Mark as no longer in progress
		delete(rq.inProgress, job.OriginalURL)
		log.Printf("Worker %d: Marked URL %s as no longer in progress", id, job.OriginalURL)

		rq.mutex.Unlock()

		totalDuration := time.Since(startTime)
		log.Printf("Worker %d: Completed job for %s in %v (render: %v, total: %v)", id, job.OriginalURL, totalDuration, renderDuration, totalDuration)
	}

	log.Printf("Render worker %d stopped (jobs channel closed)", id)
}

// GetStatus returns the current status of the render queue
func (rq *RenderQueue) GetStatus() map[string]any {
	rq.mutex.RLock()
	defer rq.mutex.RUnlock()

	inProgressURLs := make([]string, 0, len(rq.inProgress))
	for url := range rq.inProgress {
		inProgressURLs = append(inProgressURLs, url)
	}

	return map[string]any{
		"worker_count":      rq.workerCount,
		"queue_length":      len(rq.jobs),
		"in_progress_count": len(rq.inProgress),
		"in_progress_urls":  inProgressURLs,
	}
}

// Shutdown gracefully shuts down the render queue
func (rq *RenderQueue) Shutdown() {
	close(rq.jobs)
	log.Println("Render queue shutdown initiated")
}
