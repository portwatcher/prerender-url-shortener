package renderer

import (
    "context"
    "fmt"
    "log"
    "prerender-url-shortener/internal/config"
    "regexp"
    "strings"
    "time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// truncateForLog shortens long strings in logs to avoid noisy output.
func truncateForLog(s string, max int) string {
    if len(s) <= max || max <= 3 {
        return s
    }
    return s[:max] + "..."
}

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
	page.Timeout(30 * time.Second).WaitNavigation(proto.PageLifecycleEventNameNetworkAlmostIdle)()
	log.Printf("Rod: Network almost idle wait completed for URL: %s", url)

	// Give a bit of extra time for scripts to run after network idle.
	log.Printf("Rod: Additional 2-second wait for scripts to complete for URL: %s", url)
	time.Sleep(2 * time.Second)
	log.Printf("Rod: Additional wait completed for URL: %s", url)

    // Wait for metas (e.g., og:image) to reach their final state or a ready marker
	log.Printf("Rod: Waiting for meta stabilization for URL: %s", url)
	finalHTML, err := waitForMetaFinalization(page)
	if err != nil {
		log.Printf("Rod: Meta stabilization wait ended with error for URL: %s: %v. Returning current HTML.", url, err)
		// Best-effort fallback to current HTML
		html, hErr := page.HTML()
		if hErr != nil {
			return "", fmt.Errorf("failed to get HTML content after meta wait for %s: %w", url, hErr)
		}
		return html, nil
	}
	log.Printf("Rod: Meta stabilization complete for URL: %s (length: %d characters)", url, len(finalHTML))
	return finalHTML, nil
}

// waitForMetaFinalization polls the page HTML until either:
// - The configured ready marker is present in the HTML, or
// - The og:image meta content is stable for N consecutive checks (only if an og:image exists),
// or times out based on config.
func waitForMetaFinalization(page *rod.Page) (string, error) {
	timeout := time.Duration(config.AppConfig.MetaWaitTimeoutSeconds) * time.Second
	stableTarget := config.AppConfig.MetaStableConsecutiveChecks
	if stableTarget < 1 {
		stableTarget = 1
	}

    // Regex to extract meta content attributes
    // Matches either attribute form:
    //   <meta property="og:image" content="..."> OR <meta name="og:image" content="...">
    // Note: We intentionally ignore twitter:image for stabilization. The renderer remains generic
    // and will only loop for stability when an og:image is present.
    ogRe := regexp.MustCompile(`(?i)<meta[^>]+(?:property|name)=["']og:image["'][^>]*content=["']([^"']+)["'][^>]*>`) //nolint:lll

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

    var lastOG string
    stableCount := 0
    emptyCount := 0

	readyMarker := strings.TrimSpace(config.AppConfig.PrerenderReadyMarker)

	for {
		// Fetch current HTML
		html, err := page.HTML()
		if err != nil {
			return "", fmt.Errorf("failed to get HTML during meta wait: %w", err)
		}
        // html captured for checks below

		// If a custom ready marker is present, we're done
        if readyMarker != "" && strings.Contains(html, readyMarker) {
            log.Printf("Rod: Ready marker detected ('%s'); finishing render", readyMarker)
            return html, nil
        }

        // Extract og:image
        og := ""
        if m := ogRe.FindStringSubmatch(html); len(m) > 1 {
            og = m[1]
        }

        // Only loop for stability when og:image exists
        if og != "" {
            // Log first detection or changes to og:image
            if lastOG == "" {
                log.Printf("Rod: Detected og:image: %s", truncateForLog(og, 200))
                stableCount = 1
            } else if og != lastOG {
                log.Printf("Rod: og:image changed -> old: %s | new: %s", truncateForLog(lastOG, 200), truncateForLog(og, 200))
                stableCount = 1
            } else {
                stableCount++
            }
            lastOG = og
            log.Printf("Rod: og:image stability progress %d/%d", stableCount, stableTarget)
            if stableCount >= stableTarget {
                log.Printf("Rod: og:image stabilized after %d checks: %s", stableCount, truncateForLog(og, 200))
                return html, nil
            }
        } else {
            // If no og:image is present repeatedly, don't wait the full timeout.
            // This keeps the renderer generic while avoiding long waits for pages without OG metadata.
            emptyCount++
            log.Printf("Rod: No og:image detected (emptyCount %d/3)", emptyCount)
            if emptyCount >= 3 { // ~1.5s given 500ms tick
                log.Printf("Rod: Exiting early: og:image not present after brief wait")
                return html, nil
            }
        }

        if time.Now().After(deadline) {
            log.Printf("Rod: Meta wait timeout after %v. Last og:image: %s", timeout, truncateForLog(lastOG, 200))
            return html, fmt.Errorf("meta wait timeout after %v", timeout)
        }
        <-ticker.C
    }
}
