package windsurf

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
)

func (a *Agent) ReadTranscript(sessionRef string) ([]byte, error) {
	return os.ReadFile(sessionRef)
}

func (a *Agent) ChunkTranscript(content []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, errors.New("max-size must be greater than zero")
	}
	if len(content) == 0 {
		return [][]byte{{}}, nil
	}
	var chunks [][]byte
	for start := 0; start < len(content); start += maxSize {
		end := start + maxSize
		if end > len(content) {
			end = len(content)
		}
		chunk := make([]byte, end-start)
		copy(chunk, content[start:end])
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

func (a *Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	return bytes.Join(chunks, nil), nil
}

func (a *Agent) GetTranscriptPosition(path string) (int, error) {
	records, err := readTranscriptRecords(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return len(records), nil
}

func (a *Agent) ExtractModifiedFiles(path string, offset int) ([]string, int, error) {
	records, err := readTranscriptRecords(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	total := len(records)
	if offset >= total {
		return nil, total, nil
	}
	seen := make(map[string]bool)
	var files []string
	for _, rec := range records[offset:] {
		if rec.Type == transcriptTypeFile && rec.Path != "" && !seen[rec.Path] {
			seen[rec.Path] = true
			files = append(files, rec.Path)
		}
	}
	return files, total, nil
}

func (a *Agent) ExtractPrompts(path string, offset int) ([]string, error) {
	records, err := readTranscriptRecords(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var prompts []string
	for i := offset; i < len(records); i++ {
		if records[i].Type == transcriptTypePrompt && records[i].Content != "" {
			prompts = append(prompts, records[i].Content)
		}
	}
	return prompts, nil
}

func (a *Agent) ExtractSummary(path string) (string, bool, error) {
	records, err := readTranscriptRecords(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Type == transcriptTypeResponse && records[i].Content != "" {
			return records[i].Content, true, nil
		}
	}
	return "", false, nil
}

func readTranscriptRecords(path string) ([]transcriptRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseTranscriptBytes(data)
}

const scannerBufSize = 16 * 1024 * 1024 // 16 MiB — large enough for any realistic LLM response

func parseTranscriptBytes(data []byte) ([]transcriptRecord, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var records []transcriptRecord
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec transcriptRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records, scanner.Err()
}
