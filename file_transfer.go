package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

 
type FileMessage struct {
	Type        string `json:"type"`          
	Filename    string `json:"filename"`      
	FileSize    int64  `json:"file_size"`     
	ChunkNum    int    `json:"chunk_num"`     
	TotalChunks int    `json:"total_chunks"`  
	Data        string `json:"data"`          
	Checksum    string `json:"checksum"`      
}

const (
	FileChunkSize = 32 * 1024  
)

 
type FileTransferState struct {
	Filename    string
	FileSize    int64
	TotalChunks int
	Checksum    string
	Chunks      map[int][]byte  
	Progress    int             
}

 
func SendFile(filepath string, outgoing chan<- string) error {
	 
	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	 
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	filename := stat.Name()
	fileSize := stat.Size()

	 
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))

	 
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to reset file: %w", err)
	}

	 
	totalChunks := int((fileSize + FileChunkSize - 1) / FileChunkSize)

	 
	offer := FileMessage{
		Type:        "file_offer",
		Filename:    filename,
		FileSize:    fileSize,
		TotalChunks: totalChunks,
		Checksum:    checksum,
	}
	offerJSON, _ := json.Marshal(offer)
	outgoing <- string(offerJSON)

	 
	buffer := make([]byte, FileChunkSize)
	chunkNum := 0

	for {
		n, err := file.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			 
			errMsg := FileMessage{
				Type:     "file_error",
				Filename: filename,
				Data:     err.Error(),
			}
			errJSON, _ := json.Marshal(errMsg)
			outgoing <- string(errJSON)
			return fmt.Errorf("failed to read chunk: %w", err)
		}

		 
		encoded := base64.StdEncoding.EncodeToString(buffer[:n])

		chunk := FileMessage{
			Type:        "file_chunk",
			Filename:    filename,
			ChunkNum:    chunkNum,
			TotalChunks: totalChunks,
			Data:        encoded,
		}
		chunkJSON, _ := json.Marshal(chunk)
		outgoing <- string(chunkJSON)

		chunkNum++
	}

	 
	end := FileMessage{
		Type:     "file_end",
		Filename: filename,
		Checksum: checksum,
	}
	endJSON, _ := json.Marshal(end)
	outgoing <- string(endJSON)

	return nil
}

 
func SendFileFromBytes(filename string, data []byte, outgoing chan<- string) error {
	fileSize := int64(len(data))

	 
	hasher := sha256.New()
	hasher.Write(data)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	 
	totalChunks := int((fileSize + FileChunkSize - 1) / FileChunkSize)

	 
	offer := FileMessage{
		Type:        "file_offer",
		Filename:    filename,
		FileSize:    fileSize,
		TotalChunks: totalChunks,
		Checksum:    checksum,
	}
	offerJSON, _ := json.Marshal(offer)
	outgoing <- string(offerJSON)

	 
	for i := 0; i < totalChunks; i++ {
		start := i * FileChunkSize
		end := start + FileChunkSize
		if end > len(data) {
			end = len(data)
		}

		 
		encoded := base64.StdEncoding.EncodeToString(data[start:end])

		chunk := FileMessage{
			Type:        "file_chunk",
			Filename:    filename,
			ChunkNum:    i,
			TotalChunks: totalChunks,
			Data:        encoded,
		}
		chunkJSON, _ := json.Marshal(chunk)
		outgoing <- string(chunkJSON)
	}

	 
	end := FileMessage{
		Type:     "file_end",
		Filename: filename,
		Checksum: checksum,
	}
	endJSON, _ := json.Marshal(end)
	outgoing <- string(endJSON)

	return nil
}

 
func SendFolder(outgoing chan<- string) error {
	loadingBayOut := "loadingbay_out"

	 
	if _, err := os.Stat(loadingBayOut); os.IsNotExist(err) {
		return fmt.Errorf("loadingbay_out folder not found")
	}

	 
	entries, err := os.ReadDir(loadingBayOut)
	if err != nil {
		return fmt.Errorf("failed to read loadingbay_out: %w", err)
	}

	filesSent := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue  
		}

		filePath := filepath.Join(loadingBayOut, entry.Name())
		if err := SendFile(filePath, outgoing); err != nil {
			return fmt.Errorf("failed to send %s: %w", entry.Name(), err)
		}
		filesSent++
	}

	if filesSent == 0 {
		return fmt.Errorf("no files found in loadingbay_out")
	}

	return nil
}

 
func ReceiveFileChunk(msg FileMessage, transfers map[string]*FileTransferState) (*FileTransferState, error) {
	filename := msg.Filename

	switch msg.Type {
	case "file_offer":
		 
		transfers[filename] = &FileTransferState{
			Filename:    filename,
			FileSize:    msg.FileSize,
			TotalChunks: msg.TotalChunks,
			Checksum:    msg.Checksum,
			Chunks:      make(map[int][]byte),
			Progress:    0,
		}
		return transfers[filename], nil

	case "file_chunk":
		state, exists := transfers[filename]
		if !exists {
			return nil, fmt.Errorf("received chunk for unknown file: %s", filename)
		}

		 
		data, err := base64.StdEncoding.DecodeString(msg.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode chunk: %w", err)
		}

		 
		state.Chunks[msg.ChunkNum] = data
		state.Progress++

		return state, nil

	case "file_end":
		state, exists := transfers[filename]
		if !exists {
			return nil, fmt.Errorf("received file_end for unknown file: %s", filename)
		}

		 
		if state.Progress != state.TotalChunks {
			return nil, fmt.Errorf("incomplete transfer: got %d/%d chunks", state.Progress, state.TotalChunks)
		}

		 
		if err := saveFile(state, msg.Checksum); err != nil {
			return nil, err
		}

		 
		delete(transfers, filename)
		return state, nil

	case "file_error":
		delete(transfers, filename)
		return nil, fmt.Errorf("file transfer error: %s", msg.Data)

	default:
		return nil, fmt.Errorf("unknown file message type: %s", msg.Type)
	}
}

 
func saveFile(state *FileTransferState, checksum string) error {
	loadingBayIn := "loadingbay_in"

	 
	if err := os.MkdirAll(loadingBayIn, 0755); err != nil {
		return fmt.Errorf("failed to create loadingbay_in: %w", err)
	}

	 
	outPath := filepath.Join(loadingBayIn, state.Filename)
	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	 
	hasher := sha256.New()
	for i := 0; i < state.TotalChunks; i++ {
		chunk, exists := state.Chunks[i]
		if !exists {
			return fmt.Errorf("missing chunk %d", i)
		}

		if _, err := outFile.Write(chunk); err != nil {
			return fmt.Errorf("failed to write chunk %d: %w", i, err)
		}
		hasher.Write(chunk)
	}

	 
	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actualChecksum, checksum) {
		os.Remove(outPath)  
		return fmt.Errorf("checksum mismatch: expected %s, got %s", checksum, actualChecksum)
	}

	return nil
}

 
func FormatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
