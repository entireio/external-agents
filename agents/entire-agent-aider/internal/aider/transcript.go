package aider

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/entireio/external-agents/agents/entire-agent-aider/internal/protocol"
)

func (*Agent) ReadTranscript(ref string) ([]byte, error) { return os.ReadFile(ref) }
func (*Agent) ChunkTranscript(data []byte, maxSize int) ([][]byte, error) {
	if maxSize <= 0 {
		return nil, errors.New("max-size must be positive")
	}
	chunks := make([][]byte, 0)
	for len(data) > 0 {
		n := min(len(data), maxSize)
		chunks = append(chunks, bytes.Clone(data[:n]))
		data = data[n:]
	}
	return chunks, nil
}
func (*Agent) ReassembleTranscript(chunks [][]byte) ([]byte, error) {
	return bytes.Join(chunks, nil), nil
}
func (*Agent) GetTranscriptPosition(path string) (int, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, errors.New("transcript is not a regular file")
	}
	return int(info.Size()), nil
}

// visitLines retains fence state before the offset and ignores partial lines.
// Offsets are measured in original bytes, including CRLF on Windows.
func visitLines(data []byte, offset int, visit func(string)) error {
	if offset < 0 {
		return errors.New("offset must be non-negative")
	}
	fence := ""
	position := 0
	for _, raw := range bytes.SplitAfter(data, []byte("\n")) {
		line := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if fence == "" {
				fence = marker
			} else if marker == fence {
				fence = ""
			}
		} else if fence == "" && position >= offset {
			visit(line)
		}
		position += len(raw)
	}
	return nil
}
func (*Agent) ExtractPrompts(ref string, offset int) ([]string, error) {
	data, err := os.ReadFile(ref)
	if err != nil {
		return nil, err
	}
	prompts := []string{}
	err = visitLines(data, offset, func(line string) {
		if text, ok := strings.CutPrefix(line, "#### "); ok {
			if text = strings.TrimSpace(text); text != "" {
				prompts = append(prompts, text)
			}
		}
	})
	return prompts, err
}
func (*Agent) ExtractSummary(ref string) (string, bool, error) {
	_, err := os.ReadFile(ref)
	return "", false, err
}

var tokenReport = regexp.MustCompile(`(?i)^>\s*Tokens:\s*([0-9]+(?:\.[0-9]+)?[km]?)\s+sent,\s*([0-9]+(?:\.[0-9]+)?[km]?)\s+received\b`)
var costReport = regexp.MustCompile(`(?i)\bCost:\s*\$([0-9]+(?:\.[0-9]+)?)`)

func tokenCount(text string) (int64, error) {
	multiplier := float64(1)
	text = strings.ToLower(text)
	if strings.HasSuffix(text, "k") {
		multiplier = 1000
		text = strings.TrimSuffix(text, "k")
	}
	if strings.HasSuffix(text, "m") {
		multiplier = 1000000
		text = strings.TrimSuffix(text, "m")
	}
	n, err := strconv.ParseFloat(text, 64)
	n = math.Round(n * multiplier)
	if err != nil || math.IsInf(n, 0) || n >= float64(math.MaxInt64) {
		return 0, fmt.Errorf("invalid token count %q", text)
	}
	return int64(n), nil
}
func (*Agent) CalculateTokens(data []byte, offset int) (protocol.Tokens, error) {
	var usage protocol.Tokens
	var parseErr error
	err := visitLines(data, offset, func(line string) {
		if parseErr != nil {
			return
		}
		match := tokenReport.FindStringSubmatch(line)
		if match == nil {
			return
		}
		input, e := tokenCount(match[1])
		if e != nil {
			parseErr = e
			return
		}
		output, e := tokenCount(match[2])
		if e != nil {
			parseErr = e
			return
		}
		if input > math.MaxInt64-usage.InputTokens || output > math.MaxInt64-usage.OutputTokens {
			parseErr = errors.New("token count overflow")
			return
		}
		usage.InputTokens += input
		usage.OutputTokens += output
		usage.APICallCount++
		if cost := costReport.FindStringSubmatch(line); cost != nil {
			value, e := strconv.ParseFloat(cost[1], 64)
			if e != nil || math.IsInf(usage.CostUSD+value, 0) {
				parseErr = errors.New("invalid cost estimate")
				return
			}
			usage.CostUSD += value
		}
	})
	if err != nil {
		return usage, err
	}
	return usage, parseErr
}
