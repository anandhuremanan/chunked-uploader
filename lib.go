// Package chunkeduploader provides functionality for handling chunked file uploads
// with support for multiple HTTP frameworks including net/http and Gin.
package chunkeduploader

import (
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Stitches together file chunks into a single file.
// It creates a new file with a GUID as the name, and returns metadata about the stitched file.
func stitchFile(fileName string, expectedSize int64) (map[string]interface{}, error) {
	chunks := fileManager.GetChunks(fileName)

	// Create uploads directory
	uploadsDir := "./uploads"
	err := os.MkdirAll(uploadsDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("error creating uploads directory: %v", err)
	}

	// Generate GUID file name with extension
	ext := filepath.Ext(fileName)
	guid := uuid.New().String()
	storedName := guid + ext
	finalPath := filepath.Join(uploadsDir, storedName)

	finalFile, err := os.Create(finalPath)
	if err != nil {
		return nil, fmt.Errorf("error creating final file: %v", err)
	}
	defer finalFile.Close()

	var totalWritten int64

	for i, chunkPath := range chunks {
		if chunkPath == "" {
			return nil, fmt.Errorf("missing chunk %d for file %s", i, fileName)
		}

		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			return nil, fmt.Errorf("error opening chunk %d: %v", i, err)
		}

		written, err := io.Copy(finalFile, chunkFile)
		chunkFile.Close()

		if err != nil {
			return nil, fmt.Errorf("error copying chunk %d: %v", i, err)
		}

		totalWritten += written
	}

	if totalWritten != expectedSize {
		os.Remove(finalPath)
		return nil, fmt.Errorf("file size mismatch: expected %d, got %d", expectedSize, totalWritten)
	}

	// Guess MIME type
	mimeType := mime.TypeByExtension(strings.ToLower(ext))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	metadata := map[string]interface{}{
		"status":       "complete",
		"originalName": fileName,
		"storedName":   storedName,
		"fileSize":     totalWritten,
		"mimeType":     mimeType,
		"path":         finalPath,
	}

	log.Printf("Successfully stitched file: %s => %s (size: %d bytes)", fileName, storedName, totalWritten)
	return metadata, nil
}

// cleanupChunks deletes all chunks associated with a file and removes the file from the file manager.
// It logs the success or failure of each deletion.
func cleanupChunks(fileName string) {
	chunks := fileManager.GetChunks(fileName)

	for i, chunkPath := range chunks {
		if chunkPath != "" {
			err := os.Remove(chunkPath)
			if err != nil {
				log.Printf("Failed to delete chunk %d (%s): %v", i, chunkPath, err)
			} else {
				log.Printf("Successfully deleted chunk: %s", chunkPath)
			}
		}
	}

	fileManager.RemoveFile(fileName)
}

func NewFileManager() *FileManager {
	return &FileManager{
		chunks: make(map[string][]string),
	}
}

// AddChunk adds a file chunk to the file manager.
// It initializes the chunk list for the file if it doesn't exist.
func (fm *FileManager) AddChunk(fileName string, chunkPath string, chunkIndex int, totalChunks int) {
	fm.mutex.Lock()
	defer fm.mutex.Unlock()

	if _, exists := fm.chunks[fileName]; !exists {
		fm.chunks[fileName] = make([]string, totalChunks)
	}
	fm.chunks[fileName][chunkIndex] = chunkPath
}

// IsComplete checks if all chunks for a given file are present.
// It returns true if all chunks are present, false otherwise.
func (fm *FileManager) IsComplete(fileName string) bool {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()

	chunks, exists := fm.chunks[fileName]
	if !exists {
		return false
	}

	for _, chunk := range chunks {
		if chunk == "" {
			return false
		}
	}
	return true
}

// GetChunks retrieves the list of chunk paths for a given file.
// It returns a slice of strings containing the paths of the chunks.
func (fm *FileManager) GetChunks(fileName string) []string {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()
	return fm.chunks[fileName]
}

// RemoveFile removes all chunks associated with a file from the file manager.
// It deletes the entry from the chunks map.
func (fm *FileManager) RemoveFile(fileName string) {
	fm.mutex.Lock()
	defer fm.mutex.Unlock()
	delete(fm.chunks, fileName)
}

var fileManager = NewFileManager()

// uploaderUtility handles the file upload request.
// It processes multipart form data, saves file chunks, and stitches them together if all chunks are received.
func UploaderHelper(r *http.Request) (map[string]interface{}, error) {
	if r.Method != http.MethodPost {
		return nil, fmt.Errorf("method not allowed")
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, fmt.Errorf("error parsing form: %v", err)
	}

	// Get chunk metadata
	fileName := r.FormValue("fileName")
	chunkIndexStr := r.FormValue("chunkIndex")
	totalChunksStr := r.FormValue("totalChunks")
	fileSizeStr := r.FormValue("fileSize")

	if fileName == "" {
		return nil, fmt.Errorf("fileName is required")
	}

	chunkIndex, err := strconv.Atoi(chunkIndexStr)
	if err != nil {
		return nil, fmt.Errorf("invalid chunkIndex")
	}

	totalChunks, err := strconv.Atoi(totalChunksStr)
	if err != nil {
		return nil, fmt.Errorf("invalid totalChunks")
	}

	fileSize, err := strconv.ParseInt(fileSizeStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fileSize")
	}

	// Get the uploaded file
	file, _, err := r.FormFile("chunk")
	if err != nil {
		return nil, fmt.Errorf("error getting file: %v", err)
	}
	defer file.Close()

	// Create temp directory for chunks
	tempDir := "./temp_chunks"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("error creating temp directory: %v", err)
	}

	// Save chunk to temporary file
	chunkPath := filepath.Join(tempDir, fmt.Sprintf("%s_chunk_%d", fileName, chunkIndex))
	tempFile, err := os.Create(chunkPath)
	if err != nil {
		return nil, fmt.Errorf("error creating temp file: %v", err)
	}
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, file); err != nil {
		return nil, fmt.Errorf("error saving chunk: %v", err)
	}

	// Add chunk to file manager
	fileManager.AddChunk(fileName, chunkPath, chunkIndex, totalChunks)

	// Check if all chunks are received
	if fileManager.IsComplete(fileName) {
		metadata, err := stitchFile(fileName, fileSize)
		if err != nil {
			return nil, fmt.Errorf("error stitching file: %v", err)
		}

		// Clean up chunks
		cleanupChunks(fileName)

		return map[string]interface{}{
			"status":   "complete",
			"fileName": fileName,
			"message":  "File uploaded and stitched successfully",
			"metadata": metadata,
		}, nil
	}

	return map[string]interface{}{
		"status":      "chunk_received",
		"fileName":    fileName,
		"chunkIndex":  chunkIndex,
		"totalChunks": totalChunks,
	}, nil
}
