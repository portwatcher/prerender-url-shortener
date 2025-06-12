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

// hasOGImageTag checks if the HTML content contains a valid og:image meta tag
func hasOGImageTag(htmlContent string) bool {
	if htmlContent == "" {
		return false
	}

	// Convert to lowercase for case-insensitive search
	lowerHTML := strings.ToLower(htmlContent)

	// Look for meta tag with name="og:image" or property="og:image"
	// Common patterns:
	// <meta name="og:image" content="...">
	// <meta property="og:image" content="...">
	return strings.Contains(lowerHTML, `name="og:image"`) ||
		strings.Contains(lowerHTML, `property="og:image"`)
}

// GenerateShortCodeHandler handles the creation of new short URLs.
// It returns 202 Accepted for async rendering in most cases.
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

		// Check current render status
		switch existingLink.RenderStatus {
		case db.RenderStatusCompleted:
			// Check if HTML has og:image tag
			if hasOGImageTag(existingLink.RenderedHTMLContent) {
				log.Printf("URL %s has valid og:image tag, returning existing short code", req.URL)
				c.JSON(http.StatusOK, GenerateResponse{
					ShortCode:   existingLink.ShortCode,
					OriginalURL: existingLink.OriginalURL,
				})
				return
			} else {
				// No og:image tag, re-render
				log.Printf("URL %s missing og:image tag, re-queuing for render", req.URL)

				// Update status to pending and queue for re-rendering
				if updateErr := db.UpdateLinkRenderStatus(existingLink.ShortCode, db.RenderStatusPending); updateErr != nil {
					log.Printf("Error updating status to pending for re-render %s: %v", existingLink.ShortCode, updateErr)
				}

				renderer.GlobalRenderQueue.QueueRender(existingLink.ShortCode, req.URL)

				c.JSON(http.StatusAccepted, GenerateResponse{
					ShortCode:   existingLink.ShortCode,
					OriginalURL: existingLink.OriginalURL,
				})
				return
			}

		case db.RenderStatusPending, db.RenderStatusRendering:
			// Already being rendered
			log.Printf("URL %s is already being rendered (status: %s), returning 202", req.URL, existingLink.RenderStatus)
			c.JSON(http.StatusAccepted, GenerateResponse{
				ShortCode:   existingLink.ShortCode,
				OriginalURL: existingLink.OriginalURL,
			})
			return

		case db.RenderStatusFailed:
			// Previous render failed, re-queue
			log.Printf("URL %s had failed render, re-queuing", req.URL)

			// Update status to pending and queue for re-rendering
			if updateErr := db.UpdateLinkRenderStatus(existingLink.ShortCode, db.RenderStatusPending); updateErr != nil {
				log.Printf("Error updating status to pending for failed re-render %s: %v", existingLink.ShortCode, updateErr)
			}

			renderer.GlobalRenderQueue.QueueRender(existingLink.ShortCode, req.URL)

			c.JSON(http.StatusAccepted, GenerateResponse{
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

	// URL doesn't exist, generate new short code
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

	// Queue for rendering (async)
	renderer.GlobalRenderQueue.QueueRender(generatedShortCode, req.URL)

	// Return 202 Accepted immediately without waiting
	log.Printf("Queued rendering for %s, returning 202 Accepted to client", generatedShortCode)
	c.JSON(http.StatusAccepted, GenerateResponse{
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

		// For bots: only serve HTML if rendering is complete AND has valid og:image tag
		// Otherwise return 404 - bots never get redirections
		switch link.RenderStatus {
		case db.RenderStatusCompleted:
			if link.RenderedHTMLContent == "" {
				log.Printf("Bot request for %s but no rendered HTML content despite completed status. Returning 404.", shortCode)
				c.JSON(http.StatusNotFound, gin.H{"error": "Content not available"})
				return
			}

			// Check if HTML has valid og:image tag
			if hasOGImageTag(link.RenderedHTMLContent) {
				log.Printf("Bot request for %s: serving valid HTML with og:image tag", shortCode)
				c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(link.RenderedHTMLContent))
			} else {
				log.Printf("Bot request for %s but HTML missing og:image tag. Triggering re-render and returning 404.", shortCode)

				// Update status to pending and queue for re-rendering
				if updateErr := db.UpdateLinkRenderStatus(shortCode, db.RenderStatusPending); updateErr != nil {
					log.Printf("Error updating status to pending for bot-triggered re-render %s: %v", shortCode, updateErr)
				} else {
					// Queue for re-rendering
					renderer.GlobalRenderQueue.QueueRender(shortCode, link.OriginalURL)
					log.Printf("Bot-triggered re-render queued for %s", shortCode)
				}

				c.JSON(http.StatusNotFound, gin.H{"error": "Content not ready"})
			}

		case db.RenderStatusPending, db.RenderStatusRendering:
			log.Printf("Bot request for %s but rendering not complete (status: %s). Returning 404.", shortCode, link.RenderStatus)
			c.JSON(http.StatusNotFound, gin.H{"error": "Content not ready"})

		case db.RenderStatusFailed:
			log.Printf("Bot request for %s but rendering failed. Returning 404.", shortCode)
			c.JSON(http.StatusNotFound, gin.H{"error": "Content not available"})

		default:
			log.Printf("Bot request for %s with unknown render status %s. Returning 404.", shortCode, link.RenderStatus)
			c.JSON(http.StatusNotFound, gin.H{"error": "Content not available"})
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

// IndexHandler handles requests to the root path (/) by redirecting to the first allowed domain.
// If no allowed domains are configured, it returns a 404 Not Found status.
// If URL parsing fails (e.g., due to invalid characters), it returns a 500 Internal Server Error.
func IndexHandler(c *gin.Context) {

	if config.AppConfig.AllowedDomains == "" {
		c.Status(http.StatusNotFound)
		return
	}

	firstDomain := strings.Split(config.AppConfig.AllowedDomains, ",")[0]
	if firstDomain == "" {
		c.Status(http.StatusNotFound)
		return
	}

	url, err := url.Parse(fmt.Sprintf("https://%s", firstDomain))
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	c.Redirect(http.StatusFound, url.String())
}
