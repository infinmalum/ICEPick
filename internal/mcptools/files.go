package mcptools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
