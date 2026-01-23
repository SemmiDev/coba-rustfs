# RustFS Golang Demo

A demonstration application showcasing how to build a file management service using Go (Golang) that interacts with **RustFS** (an S3-compatible object storage) and **PostgreSQL**.

## Features

- **File Management**: Upload, list, download, and delete files.
- **S3 Compatibility**: Uses AWS SDK for Go v2 to communicate with RustFS.
- **Metadata Storage**: Stores file metadata (ID, name, size, upload time, etc.) in PostgreSQL.
- **Web Interface**: Simple HTML/JS frontend to interact with the API.
- **Docker Support**: Full stack orchestration using Docker Compose.

## Prerequisites

- [Docker](https://www.docker.com/) and [Docker Compose](https://docs.docker.com/compose/)
- [Go 1.25+](https://go.dev/) (if running locally without Docker for the app)

## Getting Started

### Using Docker Compose (Recommended)

The easiest way to run the entire stack (RustFS, Postgres, and the Go App) is via Docker Compose.

1.  Clone the repository:
    ```bash
    git clone https://github.com/SemmiDev/coba-rustfs.git
    cd coba-rustfs
    ```

2.  Start the services:
    ```bash
    docker compose up -d
    ```

3.  The application will be available at [http://localhost:8080](http://localhost:8080).
    -   **Web UI**: Open your browser to `http://localhost:8080`.
    -   **RustFS Console**: Available at `http://localhost:9001` (User/Pass: `rustfsadmin`/`rustfsadmin`).

### Running Locally

If you prefer to run the Go application locally while keeping dependencies in Docker:

1.  Start only the dependencies (RustFS and Postgres):
    ```bash
    docker compose up -d rustfs postgres
    ```

2.  Run the Go application:
    ```bash
    go run main.go
    ```
    *Note: The application uses default configuration values suitable for local development (localhost).*

## Configuration

The application is configured via environment variables. See `config.go` for all options.

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_PORT` | `8080` | Port for the HTTP server |
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `rustfsuser` | PostgreSQL user |
| `DB_PASSWORD` | `rustfspass` | PostgreSQL password |
| `DB_NAME` | `rustfsdb` | PostgreSQL database name |
| `RUSTFS_ENDPOINT` | `http://localhost:9000` | RustFS/S3 API Endpoint |
| `RUSTFS_ACCESS_KEY` | `rustfsadmin` | S3 Access Key |
| `RUSTFS_SECRET_KEY` | `rustfsadmin` | S3 Secret Key |
| `RUSTFS_BUCKET` | `demo-bucket` | S3 Bucket name |

## API Endpoints

You can explore the API using the provided `play.http` file or the Web UI.

-   **Health Check**: `GET /health`
-   **List Files**: `GET /api/files`
-   **Upload File**: `POST /api/files` (multipart/form-data)
-   **Get File Info**: `GET /api/files/:id`
-   **Download File**: `GET /api/files/:id/download`
-   **Delete File**: `DELETE /api/files/:id`
-   **Get Presigned URL**: `GET /api/files/:id/presigned-url`

## Project Structure

```
.
├── compose.yaml       # Docker Compose configuration
├── config.go          # Configuration loader
├── Dockerfile         # Dockerfile for the Go app
├── file_handler.go    # HTTP Handlers (Controller)
├── file_repo.go       # Database Repository
├── init.sql           # Database initialization script
├── main.go            # Entry point & setup
├── play.http          # HTTP requests for testing
├── storage_service.go # Business logic & S3 interaction
└── web/               # Static frontend files
```
