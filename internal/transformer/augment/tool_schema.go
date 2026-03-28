package augment

func normalizeToolSchema(schema map[string]interface{}) map[string]interface{} {
	if len(schema) == 0 {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	return normalizeToolSchemaProperty(schema).(map[string]interface{})
}

func normalizeToolSchemaValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, item := range v {
			out[key] = normalizeToolSchemaValue(item)
		}
		if props, ok := out["properties"].(map[string]interface{}); ok {
			for key, prop := range props {
				props[key] = normalizeToolSchemaProperty(prop)
			}
			out["properties"] = props
		}
		if items, ok := out["items"]; ok {
			out["items"] = normalizeToolSchemaProperty(items)
		}
		if req, ok := out["required"].([]string); ok {
			out["required"] = req
		}
		if items, ok := out["required"].([]interface{}); ok {
			normalized := make([]string, 0, len(items))
			for _, item := range items {
				if text, ok := item.(string); ok && text != "" {
					normalized = append(normalized, text)
				}
			}
			out["required"] = normalized
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeToolSchemaValue(item))
		}
		return out
	default:
		return value
	}
}

func normalizeToolSchemaProperty(value interface{}) interface{} {
	prop, ok := normalizeToolSchemaValue(value).(map[string]interface{})
	if !ok {
		return value
	}
	if _, hasType := prop["type"]; hasType {
		return prop
	}
	if _, hasDescription := prop["description"]; hasDescription {
		prop["type"] = "string"
		return prop
	}
	if _, hasAnyOf := prop["anyOf"]; hasAnyOf {
		return prop
	}
	if _, hasOneOf := prop["oneOf"]; hasOneOf {
		return prop
	}
	if _, hasAllOf := prop["allOf"]; hasAllOf {
		return prop
	}
	if _, hasEnum := prop["enum"]; hasEnum {
		prop["type"] = "string"
		return prop
	}
	if _, hasProps := prop["properties"]; hasProps {
		prop["type"] = "object"
		return prop
	}
	if _, hasItems := prop["items"]; hasItems {
		prop["type"] = "array"
		return prop
	}
	return prop
}
