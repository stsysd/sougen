# AGENTS.md

This file provides guidance to Coding Agents when working with code in this repository.

## Project Overview

Sougen is a Go-based web API for tracking daily activities and generating GitHub-style heatmap visualizations. It stores activity records in SQLite and provides REST endpoints for creating, reading, and visualizing data.

## Development Commands

### Building and Running
```bash
# Build the application
go build -o sougen .

# Run the application
SOUGEN_API_KEY=your_token_here ./sougen

# Run with custom configuration
SOUGEN_DATA_DIR=./data SOUGEN_SERVER_PORT=8080 SOUGEN_API_KEY=your_token_here go run main.go
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests for a specific package
go test ./store
go test ./api
go test ./model
```

### Code Quality
```bash
# Format code
go fmt ./...

# Lint code
go vet ./...

# Tidy dependencies
go mod tidy
```

## Architecture

### Core Components

- **main.go**: Application entry point that initializes config, SQLite stor, and HTTP server
- **config/**: Environment-based configuration management (data directory, port, API token)
- **model/**: Data models (Record, ProjectInfo) with validation
- **store/**: Data persistence layer with SQLite implementation
- **api/**: REST API server with authentication middleware
- **heatmap/**: SVG heatmap generation for GitHub-style visualizations

### API Structure

The API follows RESTful patterns with these main endpoints:
- `GET /healthz` - Health check (no auth required)
- `POST /v0/p/{project}/r` - Create activity record
- `GET /v0/p/{project}/r` - List records with pagination
- `GET /v0/p/{project}/graph.svg` - Generate heatmap visualization
- `DELETE /v0/p/{project}` - Delete entire project
- `DELETE /v0/r?until=DATE` - Bulk delete old records

Authentication uses `X-API-Key` header for all protected endpoints.

### Data Model

Records contain:
- UUID identifier
- Project name (activity category)
- Integer value (positive numbers only)
- Timestamp (RFC3339 format)

SQLite stores records with project/date indexing for efficient queries.

## Environment Variables

- `SOUGEN_API_KEY`: Required API authentication token
- `SOUGEN_DATA_DIR`: SQLite database directory (default: ./data)
- `SOUGEN_SERVER_PORT`: HTTP server port (default: 8080)

## Development Notes

- Code is documented in Japanese with English comments in heatmap package
- Uses standard Go project structure with clear separation of concerns
- SQLite database auto-creates tables on first run
- Heatmap generation supports custom date ranges and color schemes
- All timestamps stored as RFC3339 strings for consistency
- Use `any` instead of `interface{}`
- Simplify loop by using `slices` package if able
