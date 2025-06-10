// Package chunkeduploader provides functionality for handling chunked file uploads
// with support for multiple HTTP frameworks including net/http and Gin.
package chunkeduploader

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// Config holds configuration options for the uploader
type Config struct {
	TempDir     string // Directory for temporary chunks (default: "./temp_chunks")
	UploadsDir  string // Directory for final files (default: "./uploads")
	MaxMemory   int64  // Max memory for parsing multipart forms (default: 32MB)
	AutoCleanup bool   // Automatically cleanup chunks after stitching (default: true)
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		TempDir:     "./temp_chunks",
		UploadsDir:  "./uploads",
		MaxMemory:   32 << 20, // 32 MB
		AutoCleanup: true,
	}
}

// ChunkInfo represents metadata about a file chunk
type ChunkInfo struct {
	FileName    string `json:"fileName"`
	ChunkIndex  int    `json:"chunkIndex"`
	TotalChunks int    `json:"totalChunks"`
	FileSize    int64  `json:"fileSize"`
}

// UploadResponse represents the response from an upload operation
type UploadResponse struct {
	Status       string `json:"status"`
	FileName     string `json:"fileName"`
	ChunkIndex   int    `json:"chunkIndex,omitempty"`
	TotalChunks  int    `json:"totalChunks,omitempty"`
	Message      string `json:"message,omitempty"`
	FilePath     string `json:"filePath,omitempty"`
	ReceivedSize int64  `json:"receivedSize,omitempty"`
}

// StatusResponse represents the response from a status check
type StatusResponse struct {
	FileName       string `json:"fileName"`
	IsComplete     bool   `json:"isComplete"`
	ReceivedChunks int    `json:"receivedChunks"`
	TotalChunks    int    `json:"totalChunks"`
}

// FileManager manages file chunks and their assembly
type FileManager struct {
	chunks map[string][]string // fileName -> []chunkPaths
	mutex  sync.RWMutex
}

// NewFileManager creates a new FileManager instance
func NewFileManager() *FileManager {
	return &FileManager{
		chunks: make(map[string][]string),
	}
}

// AddChunk adds a chunk path to the manager
func (fm *FileManager) AddChunk(fileName string, chunkPath string, chunkIndex int, totalChunks int) {
	fm.mutex.Lock()
	defer fm.mutex.Unlock()

	if _, exists := fm.chunks[fileName]; !exists {
		fm.chunks[fileName] = make([]string, totalChunks)
	}
	fm.chunks[fileName][chunkIndex] = chunkPath
}

// IsComplete checks if all chunks for a file have been received
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

// GetChunks returns all chunk paths for a file
func (fm *FileManager) GetChunks(fileName string) []string {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()
	if chunks, exists := fm.chunks[fileName]; exists {
		result := make([]string, len(chunks))
		copy(result, chunks)
		return result
	}
	return nil
}

// GetReceivedChunksCount returns the number of received chunks for a file
func (fm *FileManager) GetReceivedChunksCount(fileName string) int {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()

	chunks, exists := fm.chunks[fileName]
	if !exists {
		return 0
	}

	count := 0
	for _, chunk := range chunks {
		if chunk != "" {
			count++
		}
	}
	return count
}

// RemoveFile removes a file's chunks from the manager
func (fm *FileManager) RemoveFile(fileName string) {
	fm.mutex.Lock()
	defer fm.mutex.Unlock()
	delete(fm.chunks, fileName)
}

// Uploader handles chunked file uploads
type Uploader struct {
	config      *Config
	fileManager *FileManager
}

// New creates a new Uploader instance with the given configuration
func New(config *Config) *Uploader {
	if config == nil {
		config = DefaultConfig()
	}
	return &Uploader{
		config:      config,
		fileManager: NewFileManager(),
	}
}

// NewWithDefaults creates a new Uploader instance with default configuration
func NewWithDefaults() *Uploader {
	return New(DefaultConfig())
}

// HandleUpload processes a chunked file upload
// This method is framework-agnostic and can be used with any HTTP framework
func (u *Uploader) HandleUpload(r *http.Request) (*UploadResponse, error) {
	if r.Method != http.MethodPost {
		return nil, fmt.Errorf("method not allowed")
	}

	// Parse multipart form
	err := r.ParseMultipartForm(u.config.MaxMemory)
	if err != nil {
		return nil, fmt.Errorf("error parsing form: %v", err)
	}

	// Extract chunk info
	chunkInfo, err := u.extractChunkInfo(r)
	if err != nil {
		return nil, err
	}

	// Get the uploaded file
	file, _, err := r.FormFile("chunk")
	if err != nil {
		return nil, fmt.Errorf("error getting file: %v", err)
	}
	defer file.Close()

	// Process the chunk
	return u.processChunk(chunkInfo, file)
}

// HandleUploadGin is a Gin-compatible handler wrapper
func (u *Uploader) HandleUploadGin() interface{} {
	return func(c interface{}) {
		// Type assertion for Gin context - using interface{} to avoid import dependency
		type ginContext interface {
			Request() *http.Request
			JSON(int, interface{})
			String(int, string, ...interface{})
		}

		ctx, ok := c.(ginContext)
		if !ok {
			// Fallback for direct http.ResponseWriter usage
			if w, ok := c.(http.ResponseWriter); ok {
				r := ctx.(interface{ Request() *http.Request }).Request()
				u.ServeHTTP(w, r)
				return
			}
			panic("unsupported context type")
		}

		response, err := u.HandleUpload(ctx.Request())
		if err != nil {
			ctx.String(400, "Error: %s", err.Error())
			return
		}

		ctx.JSON(200, response)
	}
}

// ServeHTTP implements http.Handler interface for direct use with net/http
func (u *Uploader) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	response, err := u.HandleUpload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetStatus returns the upload status for a file
func (u *Uploader) GetStatus(fileName string) *StatusResponse {
	return &StatusResponse{
		FileName:       fileName,
		IsComplete:     u.fileManager.IsComplete(fileName),
		ReceivedChunks: u.fileManager.GetReceivedChunksCount(fileName),
		TotalChunks:    len(u.fileManager.GetChunks(fileName)),
	}
}

// HandleStatus processes a status request
func (u *Uploader) HandleStatus(r *http.Request) (*StatusResponse, error) {
	fileName := r.URL.Query().Get("fileName")
	if fileName == "" {
		return nil, fmt.Errorf("fileName parameter is required")
	}

	return u.GetStatus(fileName), nil
}

// ServeStatusHTTP implements status checking for net/http
func (u *Uploader) ServeStatusHTTP(w http.ResponseWriter, r *http.Request) {
	response, err := u.HandleStatus(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// extractChunkInfo extracts chunk information from the request
func (u *Uploader) extractChunkInfo(r *http.Request) (*ChunkInfo, error) {
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

	return &ChunkInfo{
		FileName:    fileName,
		ChunkIndex:  chunkIndex,
		TotalChunks: totalChunks,
		FileSize:    fileSize,
	}, nil
}

// processChunk handles the processing of a single chunk
func (u *Uploader) processChunk(chunkInfo *ChunkInfo, file multipart.File) (*UploadResponse, error) {
	// Create temp directory for chunks
	err := os.MkdirAll(u.config.TempDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("error creating temp directory: %v", err)
	}

	// Save chunk to temporary file
	chunkPath := filepath.Join(u.config.TempDir, fmt.Sprintf("%s_chunk_%d", chunkInfo.FileName, chunkInfo.ChunkIndex))
	tempFile, err := os.Create(chunkPath)
	if err != nil {
		return nil, fmt.Errorf("error creating temp file: %v", err)
	}
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		return nil, fmt.Errorf("error saving chunk: %v", err)
	}

	// Add chunk to file manager
	u.fileManager.AddChunk(chunkInfo.FileName, chunkPath, chunkInfo.ChunkIndex, chunkInfo.TotalChunks)

	// Check if all chunks are received
	if u.fileManager.IsComplete(chunkInfo.FileName) {
		finalPath, err := u.stitchFile(chunkInfo.FileName, chunkInfo.FileSize)
		if err != nil {
			return nil, fmt.Errorf("error stitching file: %v", err)
		}

		// Clean up chunks if auto cleanup is enabled
		if u.config.AutoCleanup {
			u.cleanupChunks(chunkInfo.FileName)
		}

		return &UploadResponse{
			Status:       "complete",
			FileName:     chunkInfo.FileName,
			Message:      "File uploaded and stitched successfully",
			FilePath:     finalPath,
			ReceivedSize: chunkInfo.FileSize,
		}, nil
	}

	return &UploadResponse{
		Status:      "chunk_received",
		FileName:    chunkInfo.FileName,
		ChunkIndex:  chunkInfo.ChunkIndex,
		TotalChunks: chunkInfo.TotalChunks,
	}, nil
}

// stitchFile combines all chunks into the final file
func (u *Uploader) stitchFile(fileName string, expectedSize int64) (string, error) {
	chunks := u.fileManager.GetChunks(fileName)

	// Create uploads directory
	err := os.MkdirAll(u.config.UploadsDir, 0755)
	if err != nil {
		return "", fmt.Errorf("error creating uploads directory: %v", err)
	}

	// Create final file
	finalPath := filepath.Join(u.config.UploadsDir, fileName)
	finalFile, err := os.Create(finalPath)
	if err != nil {
		return "", fmt.Errorf("error creating final file: %v", err)
	}
	defer finalFile.Close()

	var totalWritten int64

	// Stitch chunks in order
	for i, chunkPath := range chunks {
		if chunkPath == "" {
			return "", fmt.Errorf("missing chunk %d for file %s", i, fileName)
		}

		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			return "", fmt.Errorf("error opening chunk %d: %v", i, err)
		}

		written, err := io.Copy(finalFile, chunkFile)
		chunkFile.Close()

		if err != nil {
			return "", fmt.Errorf("error copying chunk %d: %v", i, err)
		}

		totalWritten += written
	}

	// Verify file size
	if totalWritten != expectedSize {
		os.Remove(finalPath) // Clean up incomplete file
		return "", fmt.Errorf("file size mismatch: expected %d, got %d", expectedSize, totalWritten)
	}

	return finalPath, nil
}

// cleanupChunks removes temporary chunk files
func (u *Uploader) cleanupChunks(fileName string) {
	chunks := u.fileManager.GetChunks(fileName)
	for _, chunkPath := range chunks {
		if chunkPath != "" {
			os.Remove(chunkPath)
		}
	}
	u.fileManager.RemoveFile(fileName)
}

// CleanupFile manually cleans up chunks for a specific file
func (u *Uploader) CleanupFile(fileName string) {
	u.cleanupChunks(fileName)
}

// CORS middleware for net/http
func CORSMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	}
}
