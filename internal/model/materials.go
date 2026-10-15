package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Material blocks contain user-provided data, never system instructions.
func materialText(m Message) string {
	text := m.Content
	for _, f := range m.Files {
		if f.Text != "" {
			text += fmt.Sprintf("\n\n<attachment name=%q>\n%s\n</attachment>", f.Name, f.Text)
		}
	}
	return text
}
func materialBlocks(m Message, provider string) []any {
	kind := "text"
	if provider == "responses" {
		kind = "input_text"
	}
	blocks := []any{map[string]any{"type": kind, "text": materialText(m)}}
	for _, f := range m.Files {
		if f.Data == "" {
			continue
		}
		image := strings.HasPrefix(f.MediaType, "image/")
		uri := "data:" + f.MediaType + ";base64," + f.Data
		switch provider {
		case "responses":
			if image {
				blocks = append(blocks, map[string]any{"type": "input_image", "image_url": uri})
			} else {
				blocks = append(blocks, map[string]any{"type": "input_file", "filename": f.Name, "file_data": uri})
			}
		case "anthropic":
			typ := "document"
			if image {
				typ = "image"
			}
			blocks = append(blocks, map[string]any{"type": typ, "source": map[string]any{"type": "base64", "media_type": f.MediaType, "data": f.Data}})
		case "gemini":
			typ := "document"
			if image {
				typ = "image"
			}
			blocks = append(blocks, map[string]any{"type": typ, "mime_type": f.MediaType, "data": f.Data})
		default:
			if image {
				blocks = append(blocks, map[string]any{"type": "image_url", "image_url": map[string]any{"url": uri}})
			} else {
				blocks = append(blocks, map[string]any{"type": "file", "file": map[string]any{"filename": f.Name, "file_data": uri}})
			}
		}
	}
	return blocks
}
func materialJSON(m Message, provider string) []byte {
	item := map[string]any{"role": m.Role, "content": materialBlocks(m, provider)}
	if provider == "responses" {
		item["type"] = "message"
	}
	data, _ := json.Marshal(item)
	return data
}
