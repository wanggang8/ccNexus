package stream

import "testing"

func TestSplitSSEBundleHandlesCRLF(t *testing.T) {
	bundle := []byte("event: message\r\ndata: 1\r\n\r\n")
	chunks := splitSSEBundle(bundle)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	_, data, ok := parseSSEChunk(chunks[0])
	if !ok || data != "1" {
		t.Fatalf("expected data parsed from CRLF input, got ok=%v data=%q", ok, data)
	}
}

func TestParseSSEChunkAggregatesMultiLineData(t *testing.T) {
	chunk := []byte("event: message\ndata: first\ndata: second\n\n")
	event, data, ok := parseSSEChunk(chunk)
	if !ok {
		t.Fatalf("expected parse to succeed")
	}
	if event != "message" {
		t.Fatalf("expected event message, got %q", event)
	}
	if data != "first\nsecond" {
		t.Fatalf("expected multi-line data join, got %q", data)
	}
}

func TestParseSSEChunkIgnoresCommentLines(t *testing.T) {
	chunk := []byte(": ping\ndata: ok\n\n")
	_, data, ok := parseSSEChunk(chunk)
	if !ok || data != "ok" {
		t.Fatalf("expected comment lines ignored, got ok=%v data=%q", ok, data)
	}
}


func TestSplitSSEBundleSkipsEmptyChunks(t *testing.T) {
	bundle := []byte("\n\n\n\nevent: message\ndata: ok\n\n\n\n")
	chunks := splitSSEBundle(bundle)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	_, data, ok := parseSSEChunk(chunks[0])
	if !ok || data != "ok" {
		t.Fatalf("expected data parsed from non-empty chunk, got ok=%v data=%q", ok, data)
	}
}

func TestParseSSEChunkReturnsFalseWhenNoData(t *testing.T) {
	chunk := []byte("event: message\n\n")
	_, _, ok := parseSSEChunk(chunk)
	if ok {
		t.Fatalf("expected parse to fail when no data lines")
	}
}

func TestParseSSEChunkKeepsEventAndDataWithComments(t *testing.T) {
	chunk := []byte("event: ping\n: keep-alive\ndata: pong\n\n")
	event, data, ok := parseSSEChunk(chunk)
	if !ok || event != "ping" || data != "pong" {
		t.Fatalf("expected event/data despite comments, got event=%q data=%q ok=%v", event, data, ok)
	}
}

