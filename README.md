# Chunked File Uploader

A Go package for handling chunked file uploads with support for multiple HTTP frameworks including `net/http`, Gin, Echo, and others.

## Features

- **Framework Agnostic**: Works with any Go HTTP framework
- **Concurrent Safe**: Thread-safe chunk management
- **Configurable**: Customizable temp directories, upload directories, and memory limits
- **Auto-cleanup**: Automatic cleanup of temporary chunks after file assembly
- **Status Tracking**: Track upload progress and completion status
- **Size Verification**: Ensures uploaded file matches expected size

## Installation

```bash
go get github.com/yourusername/chunked-uploader
```

## Quick Start

### Using with net/http

```go
package main

import (
    "log"
    "net/http"
    chunkeduploader "github.com/yourusername/chunked-uploader"
)

func main() {
    // Create uploader with default config
    uploader := chunkeduploader.NewWithDefaults()

    // Set up routes
    http.HandleFunc("/upload", chunkeduploader.CORSMiddleware(uploader.ServeHTTP))
    http.HandleFunc("/status", chunkeduploader.CORSMiddleware(uploader.ServeStatusHTTP))

    log.Println("Server starting on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

### Using with Gin

```go
package main

import (
    "github.com/gin-gonic/gin"
    chunkeduploader "github.com/yourusername/chunked-uploader"
)

func main() {
    r := gin.Default()

    // Create uploader with custom config
    config := &chunkeduploader.Config{
        TempDir:     "./temp",
        UploadsDir:  "./files",
        MaxMemory:   64 << 20, // 64 MB
        AutoCleanup: true,
    }
    uploader := chunkeduploader.New(config)

    // Upload endpoint
    r.POST("/upload", func(c *gin.Context) {
        response, err := uploader.HandleUpload(c.Request)
        if err != nil {
            c.JSON(400, gin.H{"error": err.Error()})
            return
        }
        c.JSON(200, response)
    })

    // Status endpoint
    r.GET("/status", func(c *gin.Context) {
        response, err := uploader.HandleStatus(c.Request)
        if err != nil {
            c.JSON(400, gin.H{"error": err.Error()})
            return
        }
        c.JSON(200, response)
    })

    r.Run(":8080")
}
```

### Using with Echo

```go
package main

import (
    "net/http"
    "github.com/labstack/echo/v4"
    "github.com/labstack/echo/v4/middleware"
    chunkeduploader "github.com/yourusername/chunked-uploader"
)

func main() {
    e := echo.New()
    e.Use(middleware.CORS())

    uploader := chunkeduploader.NewWithDefaults()

    e.POST("/upload", func(c echo.Context) error {
        response, err := uploader.HandleUpload(c.Request())
        if err != nil {
            return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
        }
        return c.JSON(http.StatusOK, response)
    })

    e.GET("/status", func(c echo.Context) error {
        response, err := uploader.HandleStatus(c.Request())
        if err != nil {
            return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
        }
        return c.JSON(http.StatusOK, response)
    })

    e.Logger.Fatal(e.Start(":8080"))
}
```

## Configuration

```go
config := &chunkeduploader.Config{
    TempDir:     "./temp_chunks",  // Directory for temporary chunks
    UploadsDir:  "./uploads",      // Directory for final files
    MaxMemory:   32 << 20,         // Max memory for multipart parsing (32MB)
    AutoCleanup: true,             // Auto cleanup chunks after stitching
}

uploader := chunkeduploader.New(config)
```

## API Reference

### Upload Request

**Endpoint**: `POST /upload`

**Form Data**:

- `chunk`: The chunk file (multipart file)
- `fileName`: Original filename
- `chunkIndex`: Index of this chunk (0-based)
- `totalChunks`: Total number of chunks
- `fileSize`: Total size of the original file

**Response**:

```json
{
  "status": "chunk_received|complete",
  "fileName": "example.pdf",
  "chunkIndex": 0,
  "totalChunks": 5,
  "message": "File uploaded and stitched successfully",
  "filePath": "./uploads/example.pdf",
  "receivedSize": 1048576
}
```

### Status Request

**Endpoint**: `GET /status?fileName=example.pdf`

**Response**:

```json
{
  "fileName": "example.pdf",
  "isComplete": false,
  "receivedChunks": 3,
  "totalChunks": 5
}
```

## Client-Side JavaScript Example

```javascript
async function uploadFileInChunks(file, chunkSize = 1024 * 1024) {
  const chunks = Math.ceil(file.size / chunkSize);

  for (let i = 0; i < chunks; i++) {
    const start = i * chunkSize;
    const end = Math.min(start + chunkSize, file.size);
    const chunk = file.slice(start, end);

    const formData = new FormData();
    formData.append("chunk", chunk);
    formData.append("fileName", file.name);
    formData.append("chunkIndex", i.toString());
    formData.append("totalChunks", chunks.toString());
    formData.append("fileSize", file.size.toString());

    const response = await fetch("/upload", {
      method: "POST",
      body: formData,
    });

    const result = await response.json();
    console.log(`Chunk ${i + 1}/${chunks} uploaded:`, result);

    if (result.status === "complete") {
      console.log("File upload completed!");
      break;
    }
  }
}

// Usage
const fileInput = document.getElementById("fileInput");
fileInput.addEventListener("change", (e) => {
  const file = e.target.files[0];
  if (file) {
    uploadFileInChunks(file);
  }
});
```

## Advanced Usage

### Manual Cleanup

```go
uploader := chunkeduploader.New(&chunkeduploader.Config{
    AutoCleanup: false, // Disable auto cleanup
})

// Later, manually cleanup
uploader.CleanupFile("example.pdf")
```

### Custom Status Checking

```go
status := uploader.GetStatus("example.pdf")
fmt.Printf("Progress: %d/%d chunks\n", status.ReceivedChunks, status.TotalChunks)
```

## Error Handling

The package returns descriptive errors for various failure scenarios:

- Invalid form data
- Missing chunks
- File size mismatches
- IO errors during chunk processing
- Directory creation failures

## Thread Safety

The package is designed to be thread-safe and can handle concurrent uploads of different files simultaneously.

## License

MIT License
