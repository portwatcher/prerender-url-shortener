# Prerender URL Shortener

This project is a Go-based URL shortener with a prerendering feature for search engine bots and crawlers.

## Features

### 1. Web Server

The web server handles two main types of requests:

#### 1.1. `GET /<short-code>`
   - Retrieves a record from the database associated with the provided `<short-code>`.
   - **User Agent (UA) Detection:**
     - If the UA indicates a regular user browser, the server issues a redirect to the original URL.
     - If the UA indicates a bot or crawler, the server returns the pre-rendered HTML content of the original URL.

#### 1.2. `POST /generate`
   - Accepts a JSON request body with the following structure:
     ```json
     {
       "url": "string"
     }
     ```
   - Triggers the backend process to generate a short code and prerender the content.

### 2. Prerendering and Shortening Logic (Rod Integration)

When a URL is submitted via the `/generate` endpoint:
   - A unique `short-code` is generated for the given URL.
   - The system checks if this `short-code` (or the original URL) is already cached/stored in the PostgreSQL database.
   - **If not cached:**
     - The `rod` library is used to launch a headless browser instance.
     - `rod` navigates to the original URL and renders its content, ensuring support for Single Page Applications (SPAs) by waiting for JavaScript execution to complete.
     - The generated `short-code`, the JS-rendered HTML content, and the original URL are saved into the PostgreSQL database.

### 3. PostgreSQL Database

   - A PostgreSQL database is used to store the following information:
     - `short_code` (Primary Key)
     - `original_url`
     - `rendered_html_content`
     - Timestamps (e.g., `created_at`, `updated_at`)

## Technology Stack

- **Language:** Go
- **Web Framework:** Gin
- **Database:** PostgreSQL
- **ORM/DB Library:** GORM
- **Web Automation/Prerendering:** `go-rod/rod` 