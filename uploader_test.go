package chunkeduploader

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Helper function to create multipart form data for testing
func createMultipartForm(fileName string, chunkIndex, totalChunks int, fileSize int64, chunkData []byte, additionalParams string) (*http.Request, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add form fields
	writer.WriteField("fileName", fileName)
	writer.WriteField("chunkIndex", fmt.Sprintf("%d", chunkIndex))
	writer.WriteField("totalChunks", fmt.Sprintf("%d", totalChunks))
	writer.WriteField("fileSize", fmt.Sprintf("%d", fileSize))
	writer.WriteField("additionalParams", additionalParams)

	// Add file field
	part, err := writer.CreateFormFile("chunk", fmt.Sprintf("chunk_%d", chunkIndex))
	if err != nil {
		return nil, err
	}
	part.Write(chunkData)

	writer.Close()

	req := httptest.NewRequest("POST", "/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

func TestFileManager_AddChunk(t *testing.T) {
	fm := NewFileManager()
	fileName := "test.txt"
	chunkPath := "/tmp/chunk_0"

	fm.AddChunk(fileName, chunkPath, 0, 3)

	chunks := fm.GetChunks(fileName)
	if len(chunks) != 3 {
		t.Errorf("Expected 3 chunks, got %d", len(chunks))
	}

	if chunks[0] != chunkPath {
		t.Errorf("Expected chunk path %s, got %s", chunkPath, chunks[0])
	}
}

func TestFileManager_IsComplete(t *testing.T) {
	fm := NewFileManager()
	fileName := "test.txt"

	// Initially should not be complete
	if fm.IsComplete(fileName) {
		t.Error("File should not be complete initially")
	}

	// Add partial chunks
	fm.AddChunk(fileName, "/tmp/chunk_0", 0, 2)
	if fm.IsComplete(fileName) {
		t.Error("File should not be complete with partial chunks")
	}

	// Add all chunks
	fm.AddChunk(fileName, "/tmp/chunk_1", 1, 2)
	if !fm.IsComplete(fileName) {
		t.Error("File should be complete with all chunks")
	}
}

func TestFileManager_RemoveFile(t *testing.T) {
	fm := NewFileManager()
	fileName := "test.txt"

	fm.AddChunk(fileName, "/tmp/chunk_0", 0, 1)
	fm.RemoveFile(fileName)

	chunks := fm.GetChunks(fileName)
	if chunks != nil {
		t.Error("Chunks should be nil after removal")
	}
}

func TestUploaderHelper_InvalidMethod(t *testing.T) {
	req := httptest.NewRequest("GET", "/upload", nil)

	_, err := UploaderHelper(req)
	if err == nil {
		t.Error("Expected error for invalid method")
	}

	if !strings.Contains(err.Error(), "method not allowed") {
		t.Errorf("Expected 'method not allowed' error, got: %s", err.Error())
	}
}

func TestUploaderHelper_MissingFileName(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("chunkIndex", "0")
	writer.WriteField("totalChunks", "1")
	writer.WriteField("fileSize", "100")
	writer.WriteField("additionalParams", "")
	writer.Close()

	req := httptest.NewRequest("POST", "/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	_, err := UploaderHelper(req)
	if err == nil {
		t.Error("Expected error for missing fileName")
	}

	if !strings.Contains(err.Error(), "fileName is required") {
		t.Errorf("Expected 'fileName is required' error, got: %s", err.Error())
	}
}

func TestUploaderHelper_InvalidChunkIndex(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("fileName", "test.txt")
	writer.WriteField("chunkIndex", "invalid")
	writer.WriteField("totalChunks", "1")
	writer.WriteField("fileSize", "100")
	writer.WriteField("additionalParams", "")
	writer.Close()

	req := httptest.NewRequest("POST", "/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	_, err := UploaderHelper(req)
	if err == nil {
		t.Error("Expected error for invalid chunkIndex")
	}

	if !strings.Contains(err.Error(), "invalid chunkIndex") {
		t.Errorf("Expected 'invalid chunkIndex' error, got: %s", err.Error())
	}
}

func TestUploaderHelper_SingleChunkUpload(t *testing.T) {
	// Setup cleanup
	defer func() {
		os.RemoveAll("./temp_chunks")
		os.RemoveAll("./uploads")
	}()

	testData := []byte("Hello, World!")
	fileName := "test.txt"

	req, err := createMultipartForm(fileName, 0, 1, int64(len(testData)), testData, "")
	if err != nil {
		t.Fatalf("Failed to create multipart form: %v", err)
	}

	result, err := UploaderHelper(req)
	if err != nil {
		t.Fatalf("UploaderHelper failed: %v", err)
	}

	// Check result
	if result["status"] != "complete" {
		t.Errorf("Expected status 'complete', got %v", result["status"])
	}

	if result["fileName"] != fileName {
		t.Errorf("Expected fileName '%s', got %v", fileName, result["fileName"])
	}

	// Check additionalParams is empty map
	additionalParams, ok := result["additionalParams"].(map[string]interface{})
	if !ok {
		t.Fatal("additionalParams should be a map")
	}
	if len(additionalParams) != 0 {
		t.Errorf("Expected empty additionalParams, got %v", additionalParams)
	}

	// Check metadata
	metadata, ok := result["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("Metadata should be a map")
	}

	if metadata["originalName"] != fileName {
		t.Errorf("Expected originalName '%s', got %v", fileName, metadata["originalName"])
	}

	if metadata["fileSize"] != int64(len(testData)) {
		t.Errorf("Expected fileSize %d, got %v", len(testData), metadata["fileSize"])
	}

	// Verify file exists and has correct content
	storedName, ok := metadata["storedName"].(string)
	if !ok {
		t.Fatal("storedName should be a string")
	}

	filePath := filepath.Join("./uploads", storedName)
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read uploaded file: %v", err)
	}

	if !bytes.Equal(content, testData) {
		t.Errorf("File content mismatch. Expected %s, got %s", testData, content)
	}
}

func TestUploaderHelper_MultiChunkUpload(t *testing.T) {
	// Setup cleanup
	defer func() {
		os.RemoveAll("./temp_chunks")
		os.RemoveAll("./uploads")
	}()

	fileName := "test.txt"
	chunk1Data := []byte("Hello, ")
	chunk2Data := []byte("World!")
	totalSize := int64(len(chunk1Data) + len(chunk2Data))

	// Upload first chunk
	req1, err := createMultipartForm(fileName, 0, 2, totalSize, chunk1Data, "")
	if err != nil {
		t.Fatalf("Failed to create multipart form for chunk 1: %v", err)
	}

	result1, err := UploaderHelper(req1)
	if err != nil {
		t.Fatalf("UploaderHelper failed for chunk 1: %v", err)
	}

	// Should not be complete yet
	if result1["status"] != "chunk_received" {
		t.Errorf("Expected status 'chunk_received' for first chunk, got %v", result1["status"])
	}

	// Upload second chunk
	req2, err := createMultipartForm(fileName, 1, 2, totalSize, chunk2Data, "")
	if err != nil {
		t.Fatalf("Failed to create multipart form for chunk 2: %v", err)
	}

	result2, err := UploaderHelper(req2)
	if err != nil {
		t.Fatalf("UploaderHelper failed for chunk 2: %v", err)
	}

	// Should be complete now
	if result2["status"] != "complete" {
		t.Errorf("Expected status 'complete' for final chunk, got %v", result2["status"])
	}

	// Check metadata
	metadata, ok := result2["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("Metadata should be a map")
	}

	if metadata["fileSize"] != totalSize {
		t.Errorf("Expected fileSize %d, got %v", totalSize, metadata["fileSize"])
	}

	// Verify file exists and has correct content
	storedName, ok := metadata["storedName"].(string)
	if !ok {
		t.Fatal("storedName should be a string")
	}

	filePath := filepath.Join("./uploads", storedName)
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read uploaded file: %v", err)
	}

	expectedContent := append(chunk1Data, chunk2Data...)
	if !bytes.Equal(content, expectedContent) {
		t.Errorf("File content mismatch. Expected %s, got %s", expectedContent, content)
	}
}

func TestUploaderHelper_AdditionalParams(t *testing.T) {
	// Setup cleanup
	defer func() {
		os.RemoveAll("./temp_chunks")
		os.RemoveAll("./uploads")
	}()

	testData := []byte("Hello, World!")
	fileName := "test.txt"
	additionalParams := `{"userId":"123","metadata":{"type":"document","category":"test"}}`

	req, err := createMultipartForm(fileName, 0, 1, int64(len(testData)), testData, additionalParams)
	if err != nil {
		t.Fatalf("Failed to create multipart form: %v", err)
	}

	result, err := UploaderHelper(req)
	if err != nil {
		t.Fatalf("UploaderHelper failed: %v", err)
	}

	// Check that additionalParams is parsed correctly
	params, ok := result["additionalParams"].(map[string]interface{})
	if !ok {
		t.Fatal("additionalParams should be a map")
	}

	if params["userId"] != "123" {
		t.Errorf("Expected userId '123', got %v", params["userId"])
	}

	metadata, ok := params["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("metadata should be a map")
	}

	if metadata["type"] != "document" {
		t.Errorf("Expected type 'document', got %v", metadata["type"])
	}

	if metadata["category"] != "test" {
		t.Errorf("Expected category 'test', got %v", metadata["category"])
	}
}

func TestUploaderHelper_InvalidAdditionalParams(t *testing.T) {
	// Setup cleanup
	defer func() {
		os.RemoveAll("./temp_chunks")
		os.RemoveAll("./uploads")
	}()

	testData := []byte("Hello, World!")
	fileName := "test.txt"
	invalidAdditionalParams := `{"invalid": json}`

	req, err := createMultipartForm(fileName, 0, 1, int64(len(testData)), testData, invalidAdditionalParams)
	if err != nil {
		t.Fatalf("Failed to create multipart form: %v", err)
	}

	result, err := UploaderHelper(req)
	if err != nil {
		t.Fatalf("UploaderHelper failed: %v", err)
	}

	// Check that additionalParams defaults to empty map for invalid JSON
	params, ok := result["additionalParams"].(map[string]interface{})
	if !ok {
		t.Fatal("additionalParams should be a map")
	}

	if len(params) != 0 {
		t.Errorf("Expected empty additionalParams for invalid JSON, got %v", params)
	}
}

func TestUploaderHelper_ChunkReceivedWithAdditionalParams(t *testing.T) {
	// Setup cleanup
	defer func() {
		os.RemoveAll("./temp_chunks")
		os.RemoveAll("./uploads")
	}()

	fileName := "test.txt"
	chunk1Data := []byte("Hello, ")
	totalSize := int64(13) // Total size for both chunks
	additionalParams := `{"sessionId":"abc123"}`

	// Upload first chunk only
	req1, err := createMultipartForm(fileName, 0, 2, totalSize, chunk1Data, additionalParams)
	if err != nil {
		t.Fatalf("Failed to create multipart form for chunk 1: %v", err)
	}

	result1, err := UploaderHelper(req1)
	if err != nil {
		t.Fatalf("UploaderHelper failed for chunk 1: %v", err)
	}

	// Should not be complete yet
	if result1["status"] != "chunk_received" {
		t.Errorf("Expected status 'chunk_received' for first chunk, got %v", result1["status"])
	}

	// Check that additionalParams is returned
	params, ok := result1["additionalParams"].(map[string]interface{})
	if !ok {
		t.Fatal("additionalParams should be a map")
	}

	if params["sessionId"] != "abc123" {
		t.Errorf("Expected sessionId 'abc123', got %v", params["sessionId"])
	}
}

func TestStitchFile_SizeMismatch(t *testing.T) {
	// Setup cleanup
	defer func() {
		os.RemoveAll("./temp_chunks")
		os.RemoveAll("./uploads")
	}()

	fileName := "test.txt"
	chunkData := []byte("Hello")
	wrongSize := int64(100) // Wrong expected size

	// Create a temporary chunk file
	tempDir := "./temp_chunks"
	os.MkdirAll(tempDir, 0755)
	chunkPath := filepath.Join(tempDir, fmt.Sprintf("%s_chunk_0", fileName))

	err := os.WriteFile(chunkPath, chunkData, 0644)
	if err != nil {
		t.Fatalf("Failed to create test chunk: %v", err)
	}

	// Add chunk to file manager
	fileManager.AddChunk(fileName, chunkPath, 0, 1)

	// Try to stitch with wrong size
	_, err = stitchFile(fileName, wrongSize)
	if err == nil {
		t.Error("Expected error for size mismatch")
	}

	if !strings.Contains(err.Error(), "file size mismatch") {
		t.Errorf("Expected 'file size mismatch' error, got: %s", err.Error())
	}
}

func TestCleanupChunks(t *testing.T) {
	// Setup
	fileName := "test.txt"
	tempDir := "./temp_chunks"
	os.MkdirAll(tempDir, 0755)

	// Create test chunk files
	chunkPath1 := filepath.Join(tempDir, fmt.Sprintf("%s_chunk_0", fileName))
	chunkPath2 := filepath.Join(tempDir, fmt.Sprintf("%s_chunk_1", fileName))

	os.WriteFile(chunkPath1, []byte("chunk1"), 0644)
	os.WriteFile(chunkPath2, []byte("chunk2"), 0644)

	// Add chunks to file manager
	fileManager.AddChunk(fileName, chunkPath1, 0, 2)
	fileManager.AddChunk(fileName, chunkPath2, 1, 2)

	// Verify files exist
	if _, err := os.Stat(chunkPath1); os.IsNotExist(err) {
		t.Error("Chunk file 1 should exist before cleanup")
	}
	if _, err := os.Stat(chunkPath2); os.IsNotExist(err) {
		t.Error("Chunk file 2 should exist before cleanup")
	}

	// Cleanup
	cleanupChunks(fileName)

	// Verify files are deleted
	if _, err := os.Stat(chunkPath1); !os.IsNotExist(err) {
		t.Error("Chunk file 1 should be deleted after cleanup")
	}
	if _, err := os.Stat(chunkPath2); !os.IsNotExist(err) {
		t.Error("Chunk file 2 should be deleted after cleanup")
	}

	// Verify file manager entry is removed
	chunks := fileManager.GetChunks(fileName)
	if chunks != nil {
		t.Error("File manager should not have chunks after cleanup")
	}

	// Cleanup test directory
	os.RemoveAll(tempDir)
}

// Benchmark tests
func BenchmarkFileManager_AddChunk(b *testing.B) {
	fm := NewFileManager()
	fileName := "benchmark.txt"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fm.AddChunk(fileName, fmt.Sprintf("/tmp/chunk_%d", i), i, b.N)
	}
}

func BenchmarkFileManager_IsComplete(b *testing.B) {
	fm := NewFileManager()
	fileName := "benchmark.txt"

	// Setup chunks
	for i := 0; i < 100; i++ {
		fm.AddChunk(fileName, fmt.Sprintf("/tmp/chunk_%d", i), i, 100)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fm.IsComplete(fileName)
	}
}

// Test concurrent access to FileManager
func TestFileManager_ConcurrentAccess(t *testing.T) {
	fm := NewFileManager()
	fileName := "concurrent_test.txt"
	numGoroutines := 10
	chunksPerGoroutine := 10

	done := make(chan bool, numGoroutines)

	// Launch multiple goroutines to add chunks concurrently
	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			for i := 0; i < chunksPerGoroutine; i++ {
				chunkIndex := goroutineID*chunksPerGoroutine + i
				chunkPath := fmt.Sprintf("/tmp/chunk_%d", chunkIndex)
				fm.AddChunk(fileName, chunkPath, chunkIndex, numGoroutines*chunksPerGoroutine)
			}
			done <- true
		}(g)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify all chunks were added
	chunks := fm.GetChunks(fileName)
	if len(chunks) != numGoroutines*chunksPerGoroutine {
		t.Errorf("Expected %d chunks, got %d", numGoroutines*chunksPerGoroutine, len(chunks))
	}

	// Verify all chunks are present
	for i, chunk := range chunks {
		expected := fmt.Sprintf("/tmp/chunk_%d", i)
		if chunk != expected {
			t.Errorf("Chunk %d: expected %s, got %s", i, expected, chunk)
		}
	}
}
