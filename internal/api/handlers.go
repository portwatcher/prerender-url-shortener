package api

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"prerender-url-shortener/internal/config"
	"prerender-url-shortener/internal/db"
	"prerender-url-shortener/internal/renderer"
	"prerender-url-shortener/internal/shortener"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
)

// GenerateRequest is the structure for the /generate endpoint request body.
type GenerateRequest struct {
	URL string `json:"url" binding:"required,url"`
}

// GenerateResponse is the structure for the /generate endpoint response body.
type GenerateResponse struct {
	ShortCode   string `json:"short_code"`
	OriginalURL string `json:"original_url"`
}

// GenerateShortCodeHandler handles the creation of new short URLs.
// It immediately saves the short code to the database and queues rendering.
func GenerateShortCodeHandler(c *gin.Context) {
	var req GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Check if the domain is allowed
	if config.AppConfig.AllowedDomains != "" {
		parsedURL, err := url.Parse(req.URL)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid URL format: " + err.Error()})
			return
		}
		hostname := parsedURL.Hostname()

		allowedDomainsList := strings.Split(config.AppConfig.AllowedDomains, ",")
		foundMatch := slices.IndexFunc(allowedDomainsList, func(allowedDomain string) bool {
			return strings.TrimSpace(allowedDomain) == hostname
		}) != -1

		if !foundMatch {
			c.JSON(http.StatusForbidden, gin.H{"error": fmt.Sprintf("Domain '%s' is not allowed for shortening.", hostname)})
			return
		}
	}

	// Check if URL already exists in database
	existingLink, err := db.GetLinkByOriginalURL(req.URL)
	if err == nil {
		// URL already exists
		log.Printf("URL %s already exists with short code %s (status: %s)", req.URL, existingLink.ShortCode, existingLink.RenderStatus)

		// If it's already completed or failed, return immediately
		if existingLink.RenderStatus == db.RenderStatusCompleted || existingLink.RenderStatus == db.RenderStatusFailed {
			c.JSON(http.StatusOK, GenerateResponse{
				ShortCode:   existingLink.ShortCode,
				OriginalURL: existingLink.OriginalURL,
			})
			return
		}

		// If it's pending or rendering, check if we should wait or queue a new render
		if existingLink.RenderStatus == db.RenderStatusPending || existingLink.RenderStatus == db.RenderStatusRendering {
			// Check if it's currently being rendered in our queue
			if renderer.GlobalRenderQueue.IsInProgress(req.URL) {
				log.Printf("URL %s is already being rendered, waiting for completion", req.URL)
				// Wait for up to the configured timeout for rendering to complete
				timeoutDuration := time.Duration(config.AppConfig.RenderTimeoutSeconds) * time.Second
				if renderer.GlobalRenderQueue.WaitForRender(req.URL, timeoutDuration) {
					// Fetch updated link after rendering
					updatedLink, fetchErr := db.GetLinkByShortCode(existingLink.ShortCode)
					if fetchErr == nil {
						log.Printf("Existing URL rendering completed, returning ready short code to client")
						c.JSON(http.StatusOK, GenerateResponse{
							ShortCode:   updatedLink.ShortCode,
							OriginalURL: updatedLink.OriginalURL,
						})
						return
					}
				}
				// If waiting failed or timeout, just return the existing short code
				log.Printf("Timeout waiting for render of %s, returning existing short code anyway", req.URL)
			} else {
				// Not currently in queue, re-queue for rendering and wait
				log.Printf("URL %s exists but not in render queue, re-queuing and waiting", req.URL)
				renderer.GlobalRenderQueue.QueueRender(existingLink.ShortCode, req.URL)

				// Wait for the re-queued rendering to complete
				timeoutDuration := time.Duration(config.AppConfig.RenderTimeoutSeconds) * time.Second
				if renderer.GlobalRenderQueue.WaitForRender(req.URL, timeoutDuration) {
					// Fetch updated link after rendering
					updatedLink, fetchErr := db.GetLinkByShortCode(existingLink.ShortCode)
					if fetchErr == nil {
						log.Printf("Re-queued URL rendering completed, returning ready short code to client")
						c.JSON(http.StatusOK, GenerateResponse{
							ShortCode:   updatedLink.ShortCode,
							OriginalURL: updatedLink.OriginalURL,
						})
						return
					}
				}
				log.Printf("Timeout waiting for re-queued render of %s, returning existing short code anyway", req.URL)
			}

			c.JSON(http.StatusOK, GenerateResponse{
				ShortCode:   existingLink.ShortCode,
				OriginalURL: existingLink.OriginalURL,
			})
			return
		}
	} else if !gorm.IsRecordNotFoundError(err) {
		// Some other database error
		log.Printf("Error checking existing URL %s: %v", req.URL, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error while checking existing URL"})
		return
	}

	// Generate new short code
	var generatedShortCode string

	// Retry mechanism for short code generation in case of collision
	for i := range [5]struct{}{} { // Max 5 retries
		var genErr error
		generatedShortCode, genErr = shortener.GenerateShortCode()
		if genErr != nil {
			log.Printf("Error generating short code: %v", genErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate short code"})
			return
		}

		// Check if short code already exists
		_, dbErr := db.GetLinkByShortCode(generatedShortCode)
		if dbErr != nil {
			if gorm.IsRecordNotFoundError(dbErr) {
				// Code is unique, break loop
				break
			}
			// Other DB error
			log.Printf("Error checking existing short code %s: %v", generatedShortCode, dbErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error while checking short code"})
			return
		}
		// Collision, try again
		log.Printf("Short code collision for %s, retrying...", generatedShortCode)
		if i == 4 { // Check against the last index of a 5-iteration loop (0-4)
			log.Printf("Max retries reached for short code generation for URL: %s", req.URL)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate a unique short code after multiple attempts"})
			return
		}
	}

	log.Printf("Generated unique short code %s for URL: %s", generatedShortCode, req.URL)

	// Immediately save to database with pending status
	newLink := db.Link{
		ShortCode:           generatedShortCode,
		OriginalURL:         req.URL,
		RenderedHTMLContent: "", // Empty initially
		RenderStatus:        db.RenderStatusPending,
	}

	if err := db.CreateLink(&newLink); err != nil {
		log.Printf("Error creating link in database for short code %s, URL %s: %v", generatedShortCode, req.URL, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save link to database"})
		return
	}

	log.Printf("Saved link to database: %s -> %s (status: pending)", generatedShortCode, req.URL)

	// Queue for rendering
	renderer.GlobalRenderQueue.QueueRender(generatedShortCode, req.URL)

	// Wait for rendering to complete before returning to client
	log.Printf("Waiting for rendering to complete for %s before returning to client", generatedShortCode)

	// Wait for up to the configured timeout for rendering to complete
	timeoutDuration := time.Duration(config.AppConfig.RenderTimeoutSeconds) * time.Second
	if renderer.GlobalRenderQueue.WaitForRender(req.URL, timeoutDuration) {
		// Fetch updated link after rendering
		updatedLink, fetchErr := db.GetLinkByShortCode(generatedShortCode)
		if fetchErr == nil {
			if updatedLink.RenderStatus == db.RenderStatusCompleted {
				log.Printf("Rendering completed successfully for %s, returning ready short code to client", generatedShortCode)
				c.JSON(http.StatusCreated, GenerateResponse{
					ShortCode:   updatedLink.ShortCode,
					OriginalURL: updatedLink.OriginalURL,
				})
				return
			} else if updatedLink.RenderStatus == db.RenderStatusFailed {
				log.Printf("Rendering failed for %s, but returning short code anyway", generatedShortCode)
				c.JSON(http.StatusCreated, GenerateResponse{
					ShortCode:   updatedLink.ShortCode,
					OriginalURL: updatedLink.OriginalURL,
				})
				return
			}
		} else {
			log.Printf("Error fetching updated link after render wait for %s: %v", generatedShortCode, fetchErr)
		}
	} else {
		log.Printf("Timeout waiting for render completion of %s, returning short code anyway", generatedShortCode)
	}

	// Fallback: return the short code even if rendering didn't complete
	// (This handles timeout cases or other issues)
	c.JSON(http.StatusCreated, GenerateResponse{
		ShortCode:   newLink.ShortCode,
		OriginalURL: newLink.OriginalURL,
	})
}

// RedirectHandler handles requests for short URLs.
// It checks the User-Agent to either redirect to the original URL
// or serve the pre-rendered HTML.
func RedirectHandler(c *gin.Context) {
	shortCode := c.Param("shortCode")
	if shortCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Short code parameter is missing"})
		return
	}

	link, err := db.GetLinkByShortCode(shortCode)
	if err != nil {
		if gorm.IsRecordNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Short code not found"})
		} else {
			log.Printf("Error retrieving link for short code %s: %v", shortCode, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		}
		return
	}

	userAgent := c.GetHeader("User-Agent")
	// Basic check for common bot/crawler user agents. This list can be expanded.
	// Consider using a library for more robust UA parsing and bot detection.
	isBot := strings.Contains(strings.ToLower(userAgent), "bot") ||
		strings.Contains(strings.ToLower(userAgent), "crawler") ||
		strings.Contains(strings.ToLower(userAgent), "spider") ||
		strings.Contains(strings.ToLower(userAgent), "googlebot") || // More specific
		strings.Contains(strings.ToLower(userAgent), "bingbot") ||
		strings.Contains(strings.ToLower(userAgent), "slurp") || // Yahoo
		strings.Contains(strings.ToLower(userAgent), "duckduckbot") ||
		strings.Contains(strings.ToLower(userAgent), "baiduspider") ||
		strings.Contains(strings.ToLower(userAgent), "yandexbot") ||
		strings.Contains(strings.ToLower(userAgent), "facebook") || // Facebook (covers facebot and facebookexternalhit)
		strings.Contains(strings.ToLower(userAgent), "twitterbot") ||
		strings.Contains(strings.ToLower(userAgent), "linkedinbot")

	if isBot {
		log.Printf("Bot request (UA: %s) for short code: %s (render status: %s)", userAgent, shortCode, link.RenderStatus)

		// Check render status
		switch link.RenderStatus {
		case db.RenderStatusCompleted:
			if link.RenderedHTMLContent == "" {
				log.Printf("Warning: Bot request for %s but no rendered HTML content despite completed status. Redirecting instead.", shortCode)
				c.Redirect(http.StatusFound, link.OriginalURL)
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(link.RenderedHTMLContent))

		case db.RenderStatusPending, db.RenderStatusRendering:
			// For bots, we can either wait a bit or redirect immediately
			// Let's wait for a short time (5 seconds) for rendering to complete
			log.Printf("Bot request for %s but rendering not complete (status: %s), waiting briefly", shortCode, link.RenderStatus)

			// Wait for up to 5 seconds for rendering to complete
			if renderer.GlobalRenderQueue.WaitForRender(link.OriginalURL, 5*time.Second) {
				// Fetch updated link after rendering
				updatedLink, fetchErr := db.GetLinkByShortCode(shortCode)
				if fetchErr == nil && updatedLink.RenderStatus == db.RenderStatusCompleted && updatedLink.RenderedHTMLContent != "" {
					log.Printf("Bot request: rendering completed during wait, serving HTML for %s", shortCode)
					c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(updatedLink.RenderedHTMLContent))
					return
				}
			}

			// If waiting failed or rendering not complete, redirect instead
			log.Printf("Bot request: rendering not ready for %s, redirecting instead", shortCode)
			c.Redirect(http.StatusFound, link.OriginalURL)

		case db.RenderStatusFailed:
			log.Printf("Bot request for %s but rendering failed, redirecting instead", shortCode)
			c.Redirect(http.StatusFound, link.OriginalURL)

		default:
			log.Printf("Bot request for %s with unknown render status %s, redirecting instead", shortCode, link.RenderStatus)
			c.Redirect(http.StatusFound, link.OriginalURL)
		}
	} else {
		log.Printf("Redirecting user (UA: %s) for short code: %s to %s", userAgent, shortCode, link.OriginalURL)
		c.Redirect(http.StatusFound, link.OriginalURL)
	}
}

// HealthCheckHandler provides a simple health check endpoint.
func HealthCheckHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "UP"})
}

// StatusHandler provides detailed system status including render queue information.
func StatusHandler(c *gin.Context) {
	queueStatus := renderer.GlobalRenderQueue.GetStatus()

	status := gin.H{
		"status":       "UP",
		"render_queue": queueStatus,
	}

	c.JSON(http.StatusOK, status)
}
