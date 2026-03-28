package stream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type sseItem struct {
	eventName string
	payload   map[string]interface{}
}

func splitSSEBundle(bundle []byte) [][]byte {
	rawChunks := bytes.Split(bundle, []byte("\n\n"))
	chunks := make([][]byte, 0, len(rawChunks))
	for _, raw := range rawChunks {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		chunk := raw
		if !bytes.HasSuffix(chunk, []byte("\n\n")) {
			chunk = append(chunk, []byte("\n\n")...)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

func parseSSEChunk(chunk []byte) (string, string, bool) {
	lines := strings.Split(string(chunk), "\n")
	eventName := ""
	dataLines := make([]string, 0, len(lines))

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if len(dataLines) == 0 {
		return "", "", false
	}
	return eventName, strings.Join(dataLines, "\n"), true
}

func writeSSEChunk(buffer *bytes.Buffer, eventName string, payload interface{}) {
	if buffer == nil {
		return
	}
	if eventName != "" {
		buffer.WriteString("event: ")
		buffer.WriteString(eventName)
		buffer.WriteByte('\n')
	}

	switch value := payload.(type) {
	case string:
		buffer.WriteString("data: ")
		buffer.WriteString(value)
	case map[string]interface{}:
		encoded, _ := json.Marshal(value)
		buffer.WriteString("data: ")
		buffer.Write(encoded)
	default:
		encoded, _ := json.Marshal(value)
		buffer.WriteString("data: ")
		buffer.Write(encoded)
	}
	buffer.WriteString("\n\n")
}

func decodeJSONObject(body []byte) (map[string]interface{}, bool) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func cloneJSONObject(payload map[string]interface{}) map[string]interface{} {
	if payload == nil {
		return map[string]interface{}{}
	}
	cloned := make(map[string]interface{}, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func newToolCallID() string {
	return "call_" + uuid.NewString()
}
