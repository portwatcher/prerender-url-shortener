package renderer

import (
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
	var browser *rod.Browser
	var err error

	// Check if a custom rod binary path is specified
	rodBinPath := config.AppConfig.RodBinPath
	if rodBinPath != "" {
		l := launcher.New().Bin(rodBinPath)
		//nolint:errcheck
		defer l.Cleanup() // rod's Cleanup() doesn't return an error that we need to handle here.
		u, err := l.Launch()
		if err != nil {
			return "", fmt.Errorf("failed to launch rod with custom path %s: %w", rodBinPath, err)
		}
		browser = rod.New().ControlURL(u)
	} else {
		// Use default launcher (will download browser if not found)
		browser = rod.New()
	}

	err = browser.Connect()
	if err != nil {
		return "", fmt.Errorf("failed to connect to rod browser: %w", err)
	}
	//nolint:errcheck
	defer browser.MustClose() // MustClose panics on error, no return to check.

	page, err := browser.Page(proto.TargetCreateTarget{URL: url})
	if err != nil {
		return "", fmt.Errorf("failed to create page for %s: %w", url, err)
	}
	//nolint:errcheck
	defer page.MustClose() // MustClose panics on error, no return to check.

	// A common strategy is to wait for DOMContentLoaded and then a short delay for JS
	err = page.WaitLoad() // Waits for the 'load' event
	if err != nil {
		log.Printf("Error waiting for page load for %s: %v. Proceeding anyway.", url, err)
	}

	// Wait for network to be almost idle, this is a good indicator for SPAs
	// Using a timeout to prevent indefinite blocking
	//nolint:errcheck
	page.Timeout(30 * time.Second).WaitNavigation(proto.PageLifecycleEventNameNetworkAlmostIdle)()

	// Give a bit of extra time for scripts to run after network idle.
	time.Sleep(2 * time.Second)

	html, err := page.HTML()
	if err != nil {
		return "", fmt.Errorf("failed to get HTML content for %s: %w", url, err)
	}

	return html, nil
}
