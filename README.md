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
go get github.com/anandhuremanan/chunked-uploader
```

## Thread Safety

The package is designed to be thread-safe and can handle concurrent uploads of different files simultaneously.

## Known Issue
- **Cleanup Issue in Windows Servers** : In Windows The cleanup function has some bug.

## License

MIT License
