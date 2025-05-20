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

    **Explanation of `docker run` flags:**
    *   `-d`: Run the container in detached mode (in the background).
    *   `-p 8080:8080`: Map port 8080 of the host to port 8080 in the container (adjust if your `SERVER_PORT` is different).
    *   `--name prerender-shortener`: Assign a name to the container for easier management.
    *   `-e DATABASE_URL=...`: **Required.** Set the PostgreSQL connection string.
    *   `-e ALLOWED_DOMAINS=...`: Optional. Comma-separated list of domains allowed for shortening. If empty or not provided, all domains are allowed.
    *   `-e SERVER_PORT=...`: Optional. The port inside the container for the server to listen on. Defaults to `:8080` in the code, but you can set it here to ensure consistency with the `-p` flag.
    *   `-e ROD_BIN_PATH=...`: Optional. If Rod has trouble finding a suitable browser within the container, or if you build a custom image with a browser installed at a specific path, you can specify it here.
    *   `--restart unless-stopped`: Configure the container to restart automatically unless explicitly stopped.

    **Note on `DATABASE_URL` with Docker:**
    *   If your PostgreSQL database is also running in a Docker container, ensure they are on the same Docker network. You can then use the PostgreSQL container's name as the host in the `DATABASE_URL` (e.g., `postgres://user:pass@postgres-db:5432/dbname...`).
    *   If PostgreSQL is running on your Docker host machine, you might need to use a special hostname like `host.docker.internal` (for Docker Desktop) or the host's IP address on the Docker bridge network instead of `localhost`.

### Building the Docker Image (Optional)

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