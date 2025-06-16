package chunkeduploader

import "sync"

type ChunkInfo struct {
	FileName    string `json:"fileName"`
	ChunkIndex  int    `json:"chunkIndex"`
	TotalChunks int    `json:"totalChunks"`
	FileSize    int64  `json:"fileSize"`
}

type FileManager struct {
	chunks map[string][]string // fileName -> []chunkPaths
	mutex  sync.RWMutex
}
