package augment

import "strings"

const defaultImageMediaType = "image/png"

// normalizeBase64Image accepts either a raw base64 payload or a data URL.
// If the caller passes a data URL, the prefix is stripped and the embedded
// media type is returned. Otherwise the raw payload is returned with a
// fallback media type.
func normalizeBase64Image(raw, fallbackMediaType string) (data, mediaType string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	if data, mediaType, ok := splitDataURLBase64(raw); ok {
		if mediaType == "" {
			mediaType = fallbackMediaType
		}
		if mediaType == "" {
			mediaType = defaultImageMediaType
		}
		return data, mediaType
	}

	if fallbackMediaType == "" {
		fallbackMediaType = defaultImageMediaType
	}
	return raw, fallbackMediaType
}

// splitDataURLBase64 extracts the base64 payload and media type from a data URL.
// Returns ok=false when the input is not a base64 data URL.
func splitDataURLBase64(raw string) (data, mediaType string, ok bool) {
	if len(raw) < len("data:") || !strings.HasPrefix(strings.ToLower(raw), "data:") {
		return "", "", false
	}

	comma := strings.Index(raw, ",")
	if comma == -1 {
		return "", "", false
	}

	meta := raw[len("data:"):comma]
	if meta == "" {
		return "", "", false
	}

	lowerMeta := strings.ToLower(meta)
	if !strings.Contains(lowerMeta, "base64") {
		return "", "", false
	}

	segments := strings.Split(meta, ";")
	if len(segments) > 0 {
		mediaType = strings.ToLower(strings.TrimSpace(segments[0]))
	}

	data = strings.TrimSpace(raw[comma+1:])
	if data == "" {
		return "", "", false
	}

	return data, mediaType, true
}

// buildTextImageContent returns either a plain string, or a multimodal content
// array when images are present. When the text is empty, the content array will
// contain only image blocks.
func buildTextImageContent(text string, imageParts []map[string]interface{}) interface{} {
	if len(imageParts) == 0 {
		return text
	}

	parts := make([]map[string]interface{}, 0, len(imageParts)+1)
	if text != "" {
		parts = append(parts, map[string]interface{}{"type": "text", "text": text})
	}
	parts = append(parts, imageParts...)

	if len(parts) == 1 && text != "" {
		return text
	}
	return parts
}

// buildClaudeImageBlock converts a raw image payload into the Claude image block format.
// The raw value may be either a bare base64 payload or a full data URL.
func buildClaudeImageBlock(raw, fallbackMediaType string) map[string]interface{} {
	data, mediaType := normalizeBase64Image(raw, fallbackMediaType)
	if data == "" {
		return nil
	}
	if mediaType == "" {
		mediaType = defaultImageMediaType
	}
	return map[string]interface{}{
		"type": "image",
		"source": map[string]interface{}{
			"type":       "base64",
			"media_type": mediaType,
			"data":       data,
		},
	}
}

// buildOpenAIImagePart converts a raw image payload into the OpenAI multimodal content format.
// The raw value may be either a bare base64 payload or a full data URL.
func buildOpenAIImagePart(raw, fallbackMediaType string) map[string]interface{} {
	data, mediaType := normalizeBase64Image(raw, fallbackMediaType)
	if data == "" {
		return nil
	}
	if mediaType == "" {
		mediaType = defaultImageMediaType
	}
	return map[string]interface{}{
		"type": "image_url",
		"image_url": map[string]interface{}{
			"url": "data:" + mediaType + ";base64," + data,
		},
	}
}
