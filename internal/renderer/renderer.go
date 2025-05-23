package renderer

import (
	"context"
	"fmt"
	"log"
	"prerender-url-shortener/internal/config"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// RenderPageWithRod fetches a URL using Rod, waits for JavaScript to render (basic wait),
// and returns the full HTML content.
func RenderPageWithRod(url string) (string, error) {
	log.Printf("Rod rendering started for URL: %s", url)

	// Set overall timeout for the entire rendering process
	timeoutDuration := time.Duration(config.AppConfig.RenderTimeoutSeconds) * time.Second
	log.Printf("Rod: Using render timeout of %v for URL: %s", timeoutDuration, url)
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancel()

	// Create a channel to handle the result
	resultChan := make(chan struct {
		html string
		err  error
	}, 1)

	// Run the rendering in a goroutine to enable timeout
	go func() {
		html, err := renderWithRod(url)
		select {
		case resultChan <- struct {
			html string
			err  error
		}{html, err}:
		case <-ctx.Done():
			log.Printf("Rod: Rendering goroutine cancelled for URL: %s", url)
		}
	}()

	// Wait for result or timeout
	select {
	case result := <-resultChan:
		if result.err != nil {
			log.Printf("Rod: Rendering failed for URL: %s, error: %v", url, result.err)
		} else {
			log.Printf("Rod: Rendering completed successfully for URL: %s", url)
		}
		return result.html, result.err
	case <-ctx.Done():
		log.Printf("Rod: Rendering timeout after %v for URL: %s", timeoutDuration, url)
		return "", fmt.Errorf("rendering timeout after %v for URL: %s", timeoutDuration, url)
	}
}

// renderWithRod is the actual rendering implementation
func renderWithRod(url string) (string, error) {
	var browser *rod.Browser
	var err error

	// Check if a custom rod binary path is specified
	rodBinPath := config.AppConfig.RodBinPath
	if rodBinPath != "" {
		log.Printf("Rod: Using custom binary path: %s for URL: %s", rodBinPath, url)
		l := launcher.New().Bin(rodBinPath)
		//nolint:errcheck
		defer l.Cleanup() // rod's Cleanup() doesn't return an error that we need to handle here.

		log.Printf("Rod: Launching browser with custom path for URL: %s", url)
		u, err := l.Launch()
		if err != nil {
			return "", fmt.Errorf("failed to launch rod with custom path %s: %w", rodBinPath, err)
		}
		log.Printf("Rod: Browser launched successfully with custom path for URL: %s", url)
		browser = rod.New().ControlURL(u)
	} else {
		// Use default launcher (will download browser if not found)
		log.Printf("Rod: Using default browser launcher for URL: %s", url)
		browser = rod.New()
	}

	log.Printf("Rod: Connecting to browser for URL: %s", url)
	err = browser.Connect()
	if err != nil {
		return "", fmt.Errorf("failed to connect to rod browser: %w", err)
	}
	log.Printf("Rod: Successfully connected to browser for URL: %s", url)
	//nolint:errcheck
	defer func() {
		log.Printf("Rod: Closing browser for URL: %s", url)
		browser.MustClose() // MustClose panics on error, no return to check.
		log.Printf("Rod: Browser closed for URL: %s", url)
	}()

	log.Printf("Rod: Creating new page for URL: %s", url)
	page, err := browser.Page(proto.TargetCreateTarget{URL: url})
	if err != nil {
		return "", fmt.Errorf("failed to create page for %s: %w", url, err)
	}
	log.Printf("Rod: Page created successfully for URL: %s", url)
	//nolint:errcheck
	defer func() {
		log.Printf("Rod: Closing page for URL: %s", url)
		page.MustClose() // MustClose panics on error, no return to check.
		log.Printf("Rod: Page closed for URL: %s", url)
	}()

	// A common strategy is to wait for DOMContentLoaded and then a short delay for JS
	log.Printf("Rod: Waiting for page load event for URL: %s", url)
	err = page.WaitLoad() // Waits for the 'load' event
	if err != nil {
		log.Printf("Rod: Error waiting for page load for %s: %v. Proceeding anyway.", url, err)
	} else {
		log.Printf("Rod: Page load event completed for URL: %s", url)
	}

	// Wait for network to be almost idle, this is a good indicator for SPAs
	// Using a timeout to prevent indefinite blocking
	log.Printf("Rod: Waiting for network to be almost idle for URL: %s (timeout: 30s)", url)
	//nolint:errcheck
	page.Timeout(30 * time.Second).WaitNavigation(proto.PageLifecycleEventNameNetworkAlmostIdle)()
	log.Printf("Rod: Network almost idle wait completed for URL: %s", url)

	// Give a bit of extra time for scripts to run after network idle.
	log.Printf("Rod: Additional 2-second wait for scripts to complete for URL: %s", url)
	time.Sleep(2 * time.Second)
	log.Printf("Rod: Additional wait completed for URL: %s", url)

	log.Printf("Rod: Extracting HTML content for URL: %s", url)
	html, err := page.HTML()
	if err != nil {
		return "", fmt.Errorf("failed to get HTML content for %s: %w", url, err)
	}
	log.Printf("Rod: Successfully extracted HTML content for URL: %s (length: %d characters)", url, len(html))

	return html, nil
}
