# GoFi - Simple & Secure File Sharing

[![Go Version](https://img.shields.io/badge/Go-1.24+-blue.svg)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

GoFi is a lightweight, self-hostable file sharing service built with Go. It provides a simple and secure way to upload, download, and share files, with an optional short link generation feature.

## Features

- **Secure File Uploads**: Upload files to public or private storage directories, protected by token-based authentication.
- **Direct Downloads**: Access files directly via their filenames.
- **Short Link Generation**: Create unique, short URLs for easy file sharing.
- **PostgreSQL Backend**: Uses a robust PostgreSQL database to store file metadata and short links.
- **Flexible Configuration**: Configure the application using a TOML file or environment variables.
- **Dockerized**: Comes with a `docker-compose.yml` for easy setup of the required PostgreSQL database.
- **API Documentation**: Includes Swagger for clear, interactive API documentation.

## Getting Started

### Prerequisites

- Go 1.24.2 or later
- PostgreSQL

### Installation

1. **Clone the repository:**

    ```sh
    git clone https://github.com/ShinoharaHaruna/GoFi.git
    cd GoFi
    ```

2. **Install dependencies:**

    ```sh
    go mod tidy
    ```

3. **Configure the application:**
    Create a `config.toml` file in the root directory by copying the `config.toml.template` and filling in your details, especially the `DATABASE_URL`.

4. **Run the application:**

    ```sh
    go run ./cmd/gofi/main.go
    ```

    The server will start on the port specified in your configuration (default is `8080`).

## Configuration

GoFi can be configured via a `config.toml` file or through environment variables. Environment variables take precedence.

| Variable             | TOML Key             | Environment Variable | Default Value     | Description                                                                 |
| -------------------- | -------------------- | -------------------- | ----------------- | --------------------------------------------------------------------------- |
| **Gin Mode**         | `GIN_MODE`           | `GOFI_GIN_MODE`      | `debug`           | The run mode for the Gin framework (`debug`, `release`, `test`).              |
| **Server Port**      | `GOFI_PORT`          | `GOFI_PORT`          | `8080`            | The port on which the server will listen.                                   |
| **Base Directory**   | `GOFI_BASE_DIR`      | `GOFI_BASE_DIR`      | `./data`          | The root directory where uploaded files will be stored.                     |
| **Database URL**     | `DATABASE_URL`       | `GOFI_DATABASE_URL`  | `""`              | The connection string for the PostgreSQL database.                          |

**Example `config.toml`:**

```toml
GIN_MODE = "release"
GOFI_PORT = "8080"
GOFI_BASE_DIR = "./data"
DATABASE_URL = "postgres://gofi_user:gofi_local_dev@localhost:54320/gofi_db?sslmode=disable"
```

## API Usage

GoFi provides a RESTful API for all its operations. For detailed information about endpoints, request/response formats, and to try out the API live, please refer to our Swagger documentation.

Once the server is running, you can access the Swagger UI at:

**<http://localhost:8080/swagger/index.html>**

### Key Endpoints

- `POST /upload`: Upload a file.
- `GET /:filename`: Download a file by its name.
- `POST /shorten`: Create a short link for a file.
- `GET /s/:shortcode`: Download a file using its short link.

### Initial API Keys

Certain endpoints require API keys. After the database is initialized, insert keys into the `api_keys` table for each usage type:

```sql
INSERT INTO api_keys (key, type, is_enabled)
VALUES
  ('<your-upload-key>', 'upload', true),
  ('<your-download-key>', 'download', true),
  ('<your-shorten-key>', 'shorten', true);
```

Each key controls access to the matching feature:

1. **upload** – required when calling `POST /upload`.
2. **download** – required when accessing private files or short links pointing to private files.
3. **shorten** – required for `POST /shorten`, `DELETE /shorten/:shortcode`, and `POST /shorten/:shortcode/enable`.

## Docker Support

This project includes a `docker-compose.yml` file to easily set up a PostgreSQL database for local development.

To start the database service, run:

```sh
docker compose up -d
```

This will start a PostgreSQL container with the following default credentials:

- **Database**: `gofi_db`
- **User**: `gofi_user`
- **Password**: `gofi_local_dev`
- **Port**: `54320` (on the host machine)

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
