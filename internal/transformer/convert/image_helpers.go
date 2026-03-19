package convert

import (
	"fmt"
	"strings"
)

func splitDataURLImage(raw string) (mediaType, data string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "data:") {
		return "", "", false
	}

	parts := strings.SplitN(raw, ",", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	meta := strings.TrimPrefix(parts[0], "data:")
	metaParts := strings.Split(meta, ";")
	if len(metaParts) > 0 {
		mediaType = strings.TrimSpace(metaParts[0])
	}
	if mediaType == "" {
		mediaType = "image/png"
	}
	return mediaType, parts[1], true
}

func normalizeImageURL(raw interface{}) (string, bool) {
	switch v := raw.(type) {
	case string:
		v = strings.TrimSpace(v)
		return v, v != ""
	case map[string]interface{}:
		if url, ok := v["url"].(string); ok {
			url = strings.TrimSpace(url)
			if url != "" {
				return url, true
			}
		}
		if url, ok := v["image_url"].(string); ok {
			url = strings.TrimSpace(url)
			if url != "" {
				return url, true
			}
		}
		if imgObj, ok := v["image_url"].(map[string]interface{}); ok {
			if url, ok := imgObj["url"].(string); ok {
				url = strings.TrimSpace(url)
				if url != "" {
					return url, true
				}
			}
			if url, ok := imgObj["image_url"].(string); ok {
				url = strings.TrimSpace(url)
				if url != "" {
					return url, true
				}
			}
		}
		if source, ok := v["source"].(map[string]interface{}); ok {
			if url, ok := source["url"].(string); ok {
				url = strings.TrimSpace(url)
				if url != "" {
					return url, true
				}
			}
			if data, ok := source["data"].(string); ok {
				data = strings.TrimSpace(data)
				if data != "" {
					mediaType, _ := source["media_type"].(string)
					mediaType = strings.TrimSpace(mediaType)
					if mediaType == "" {
						mediaType = "image/png"
					}
					return fmt.Sprintf("data:%s;base64,%s", mediaType, data), true
				}
			}
		}
	}
	return "", false
}

func openAIChatImagePartFromURL(url string) map[string]interface{} {
	if strings.TrimSpace(url) == "" {
		return nil
	}
	return map[string]interface{}{
		"type": "image_url",
		"image_url": map[string]interface{}{
			"url": url,
		},
	}
}

func openAI2ImagePartFromURL(url string) map[string]interface{} {
	if strings.TrimSpace(url) == "" {
		return nil
	}
	return map[string]interface{}{
		"type":      "input_image",
		"image_url": url,
	}
}

func claudeImageBlockFromURL(url string) map[string]interface{} {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil
	}
	if mediaType, data, ok := splitDataURLImage(url); ok {
		return map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": mediaType,
				"data":       data,
			},
		}
	}
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type": "url",
				"url":  url,
			},
		}
	}
	return nil
}

func geminiInlineDataFromURL(url string) map[string]interface{} {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil
	}
	mediaType, data, ok := splitDataURLImage(url)
	if !ok {
		return nil
	}
	return map[string]interface{}{
		"inlineData": map[string]interface{}{
			"mimeType": mediaType,
			"data":     data,
		},
	}
}
