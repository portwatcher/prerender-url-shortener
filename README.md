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

### 2. Prerendering and Shortening Logic (Rod Integration with Async Queue)

When a URL is submitted via the `/generate` endpoint:
   - A unique `short-code` is generated for the given URL.
   - The system checks if this URL is already cached/stored in the PostgreSQL database.
   - **If not cached:**
     - The link is immediately saved to the database with a "pending" render status.
     - The response is returned immediately with the short code.
     - The URL is queued for background rendering using a worker pool.
   - **If already exists:**
     - If rendering is complete, returns the existing short code.
     - If rendering is in progress, waits briefly and returns the existing short code.
     - Prevents duplicate rendering of the same URL.
   
   **Background Rendering Process:**
   - Configurable number of worker goroutines process the render queue.
   - Each worker uses the `rod` library to launch a headless browser instance.
   - `rod` navigates to the original URL and renders its content, ensuring support for Single Page Applications (SPAs).
   - The rendered HTML content and status are updated in the database upon completion.

### 3. PostgreSQL Database

   - A PostgreSQL database is used to store the following information:
     - `short_code` (Primary Key)
     - `original_url` (Indexed for efficient lookups)
     - `rendered_html_content`
     - `render_status` (pending, rendering, completed, failed)
     - Timestamps (e.g., `created_at`, `updated_at`)

### 4. Additional Endpoints

#### 4.1. `GET /health`
   - Simple health check endpoint returning `{"status": "UP"}`

#### 4.2. `GET /status`
   - Detailed status endpoint including render queue information:
     ```json
     {
       "status": "UP",
       "render_queue": {
         "worker_count": 3,
         "queue_length": 2,
         "in_progress_count": 1,
         "in_progress_urls": ["https://example.com"],
         "waiting_goroutines": 0
       }
     }
     ```

## Technology Stack

- **Language:** Go
- **Web Framework:** Gin
- **Database:** PostgreSQL
- **ORM/DB Library:** GORM
- **Web Automation/Prerendering:** `go-rod/rod`

## Getting Started

### Prerequisites

- Go (version 1.24.1 or higher)
- PostgreSQL database
- Docker (optional, for containerized deployment)

### Manual Installation & Running

1.  **Clone the repository:**

```bash
git clone <repository-url>
cd prerender-url-shortener
```
2.  **Create a `.env` file** in the project root with your configuration. See `.env.example` for a template (if one exists, otherwise define the following):

```env
DATABASE_URL="postgres://user:password@host:port/dbname?sslmode=disable"
SERVER_PORT=":8080" # Optional, defaults to :8080
ALLOWED_DOMAINS="example.com,another.org" # Optional, comma-separated, empty means allow all
ROD_BIN_PATH="" # Optional, path to Chrome/Chromium binary if not in system PATH or for specific version
RENDER_WORKER_COUNT="3" # Optional, number of background rendering workers, defaults to 3
```

3.  **Install dependencies:**
```bash
go mod tidy
```
4.  **Run the application:**

```bash
go run cmd/server/main.go
```
The server will start, typically on port 8080.

### Running with Docker

A multi-architecture Docker image (supporting `linux/amd64` and `linux/arm64`) is available on Docker Hub at `juryschon/prerender-url-shortener:latest`.

1.  **Pull the image (optional, `docker run` will do it automatically):**

```bash
docker pull juryschon/prerender-url-shortener:latest
```

2.  **Run the container:**

```bash
docker run -d -p 8080:8080 \
  --name prerender-shortener \
  -e DATABASE_URL="postgres://your_user:your_password@your_db_host:5432/your_dbname?sslmode=disable" \
  -e ALLOWED_DOMAINS="example.com,another.org" \
  -e SERVER_PORT=":8080" \
  # -e ROD_BIN_PATH="/usr/bin/google-chrome-stable" # Optional: if you bake chrome into your image and rod can't find it
  --restart unless-stopped \
  juryschon/prerender-url-shortener:latest
```

### Building the Docker Image

If you want to build the image yourself:

1.  Ensure Docker Buildx is enabled (often default in Docker Desktop, or run `docker buildx create --use`).
2.  Log in to Docker Hub if you intend to push:

```bash
docker login
```

3.  Run the build command:

```bash
# For multi-platform (amd64, arm64) and pushing to your Docker Hub (replace <your_username>)
docker buildx build --platform linux/amd64,linux/arm64 -t <your_username>/prerender-url-shortener:latest --push .

# For a local build (current platform only)
docker build -t prerender-url-shortener .
``` 