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

// GenerateShortCodeHandler handles the creation of new short URLs.
// It takes a URL, generates a short code, renders the page with Rod,
// and saves it to the database.
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

	// TODO: Check if URL already exists and return existing short code if so.
	// This requires a way to efficiently query by OriginalURL.
	// For now, we always generate a new one, which might lead to duplicates
	// if the same URL is submitted multiple times.

	var newLink db.Link
	var err error
	var generatedShortCode string

	// Retry mechanism for short code generation in case of collision (though unlikely with 6 chars)
	for i := range [5]struct{}{} { // Max 5 retries, modernized loop
		generatedShortCode, err = shortener.GenerateShortCode()
		if err != nil {
			log.Printf("Error generating short code: %v", err)
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

	// Render the page using Rod
	// This can be a long operation, consider running it in a goroutine for production
	// and returning a 202 Accepted immediately. For now, synchronous.
	log.Printf("Starting Rod rendering for URL: %s", req.URL)
	htmlContent, err := renderer.RenderPageWithRod(req.URL)
	if err != nil {
		log.Printf("Error rendering page with Rod for URL %s: %v", req.URL, err)
		// Decide if we still want to save the link without prerendered content
		// For now, we'll return an error and not save.
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to render page content: %v", err)})
		return
	}
	log.Printf("Successfully rendered HTML for URL: %s (length: %d)", req.URL, len(htmlContent))

	newLink = db.Link{
		ShortCode:           generatedShortCode,
		OriginalURL:         req.URL,
		RenderedHTMLContent: htmlContent,
		// CreatedAt and UpdatedAt are handled by gorm.Model automatically
	}

	if err := db.CreateLink(&newLink); err != nil {
		log.Printf("Error creating link in database for short code %s, URL %s: %v", generatedShortCode, req.URL, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save link to database"})
		return
	}

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
		strings.Contains(strings.ToLower(userAgent), "facebot") || // Facebook
		strings.Contains(strings.ToLower(userAgent), "twitterbot") ||
		strings.Contains(strings.ToLower(userAgent), "linkedinbot")

	if isBot {
		log.Printf("Serving prerendered content for bot (UA: %s) for short code: %s", userAgent, shortCode)
		if link.RenderedHTMLContent == "" {
			// This case should ideally not happen if generation was successful
			// but handle it just in case. Redirecting might be a better fallback.
			log.Printf("Warning: Bot request for %s but no rendered HTML available. Redirecting instead.", shortCode)
			c.Redirect(http.StatusFound, link.OriginalURL)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(link.RenderedHTMLContent))
	} else {
		log.Printf("Redirecting user (UA: %s) for short code: %s to %s", userAgent, shortCode, link.OriginalURL)
		c.Redirect(http.StatusFound, link.OriginalURL)
	}
}

// HealthCheckHandler provides a simple health check endpoint.
func HealthCheckHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "UP"})
}
