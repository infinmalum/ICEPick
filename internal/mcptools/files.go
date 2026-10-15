package mcptools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// maxReadFileBytes bounds a single read_file call. Larger or non-regular
// files are refused instead of being truncated silently: the model should
// see an error it can reason about.
const maxReadFileBytes = 64 * 1024

// readLocalFile is the execution layer of local__read_file.
func readLocalFile(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if path == "" {
		return "", errors.New("缺少文件路径")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("解析路径失败: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}
	if info.IsDir() {
		f, err := os.Open(resolved)
		if err != nil {
			return "", err
		}
		defer f.Close()
		entries, err := f.ReadDir(201)
		if err != nil && err != io.EOF {
			return "", err
		}
		truncated := len(entries) > 200
		if truncated {
			entries = entries[:200]
		}
		rows := []map[string]any{}
		for _, entry := range entries {
			rows = append(rows, map[string]any{"name": entry.Name(), "directory": entry.IsDir(), "symlink": entry.Type()&os.ModeSymlink != 0})
		}
		raw, err := json.Marshal(map[string]any{"path": resolved, "entries": rows, "truncated": truncated, "note": "按需读取相关子路径，不自动递归载入全部内容"})
		return string(raw), err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("目标不是常规文件")
	}
	if info.Size() > maxReadFileBytes {
		return "", fmt.Errorf("文件超过 %d 字节读取上限", maxReadFileBytes)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", errors.New("仅支持读取文本文件")
	}
	return string(data), nil
}
