package chunkeduploader

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
)

func TestUploaderBasicFunctionality(t *testing.T) {
	// Clean up test directories
	defer func() {
		os.RemoveAll("./test_temp_chunks")
		os.RemoveAll("./test_uploads")
	}()

	config := &Config{
		TempDir:     "./test_temp_chunks",
		UploadsDir:  "./test_uploads",
		MaxMemory:   32 << 20,
		AutoCleanup: true,
	}

	uploader := New(config)

	// Test data
	testData := []byte("Hello, World! This is a test file content.")
	fileName := "test.txt"
	chunkSize := 15
	totalChunks := (len(testData) + chunkSize - 1) / chunkSize

	// Upload chunks
	for i := 0; i < totalChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(testData) {
			end = len(testData)
		}
		chunk := testData[start:end]

		// Create multipart form
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Add form fields
		writer.WriteField("fileName", fileName)
		writer.WriteField("chunkIndex", strconv.Itoa(i))
		writer.WriteField("totalChunks", strconv.Itoa(totalChunks))
		writer.WriteField("fileSize", strconv.FormatInt(int64(len(testData)), 10))

		// Add file field
		part, err := writer.CreateFormFile("chunk", fmt.Sprintf("chunk_%d", i))
		if err != nil {
			t.Fatalf("Error creating form file: %v", err)
		}
		part.Write(chunk)
		writer.Close()

		// Create request
		req := httptest.NewRequest("POST", "/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		// Handle upload
		response, err := uploader.HandleUpload(req)
		if err != nil {
			t.Fatalf("Error handling upload for chunk %d: %v", i, err)
		}

		if i < totalChunks-1 {
			if response.Status != "chunk_received" {
				t.Errorf("Expected status 'chunk_received' for chunk %d, got %s", i, response.Status)
			}
		} else {
			if response.Status != "complete" {
				t.Errorf("Expected status 'complete' for final chunk, got %s", response.Status)
			}
		}
	}

	// Verify the final file
	finalPath := "./test_uploads/" + fileName
	if _, err := os.Stat(finalPath); os.IsNotExist(err) {
		t.Fatalf("Final file was not created: %s", finalPath)
	}

	// Read and verify content
	content, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("Error reading final file: %v", err)
	}

	if !bytes.Equal(content, testData) {
		t.Errorf("File content mismatch. Expected: %s, Got: %s", string(testData), string(content))
	}
}

func TestStatusFunctionality(t *testing.T) {
	uploader := NewWithDefaults()

	fileName := "nonexistent.txt"
	status := uploader.GetStatus(fileName)

	if status.IsComplete {
		t.Error("Non-existent file should not be marked as complete")
	}

	if status.ReceivedChunks != 0 {
		t.Errorf("Expected 0 received chunks, got %d", status.ReceivedChunks)
	}
}

func TestHTTPHandler(t *testing.T) {
	uploader := NewWithDefaults()

	// Test with invalid method
	req := httptest.NewRequest("GET", "/upload", nil)
	rec := httptest.NewRecorder()

	uploader.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for GET request, got %d", rec.Code)
	}
}

// example_basic_test.go - Example usage
func ExampleUploader_basic() {
	// Create uploader with default configuration
	uploader := NewWithDefaults()

	// Use with net/http
	http.HandleFunc("/upload", CORSMiddleware(uploader.ServeHTTP))
	http.HandleFunc("/status", CORSMiddleware(uploader.ServeStatusHTTP))

	fmt.Println("Uploader configured successfully")
	// Output: Uploader configured successfully
}

func ExampleUploader_withConfig() {
	// Create uploader with custom configuration
	config := &Config{
		TempDir:     "./my_temp",
		UploadsDir:  "./my_uploads",
		MaxMemory:   64 << 20, // 64 MB
		AutoCleanup: true,
	}

	uploader := New(config)

	// Check status
	status := uploader.GetStatus("example.pdf")
	fmt.Printf("File complete: %v\n", status.IsComplete)
	// Output: File complete: false
}
