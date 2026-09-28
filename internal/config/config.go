package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
)

type Paths struct {
	Config string
	Data   string
}

func UserPaths() Paths {
	return Paths{
		Config: filepath.Join(xdg.ConfigHome, "ICEPick", "config.json"),
		Data:   filepath.Join(xdg.DataHome, "ICEPick"),
	}
}

type Config struct {
	Model Model `json:"model"`
}

type Model struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Name     string `json:"name"`
}

func Default() Config {
	return Config{Model: Model{Provider: "ollama", BaseURL: "http://127.0.0.1:11434/v1"}}
}

// Load reads a user-level config. A missing file gets safe local defaults;
// nothing is sent to the configured endpoint until the model adapter exists.
func Load(path string) (Config, error) {
	result := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("读取配置: %w", err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Config{}, fmt.Errorf("解析配置: %w", err)
	}
	if result.Model.Provider != "ollama" {
		return Config{}, fmt.Errorf("暂不支持模型提供方 %q", result.Model.Provider)
	}
	if !strings.HasPrefix(result.Model.BaseURL, "http://127.0.0.1:") && !strings.HasPrefix(result.Model.BaseURL, "http://localhost:") {
		return Config{}, errors.New("当前阶段模型地址必须指向本机 HTTP 服务")
	}
	return result, nil
}

func EnsureDirs(paths Paths) error {
	for _, dir := range []string{filepath.Dir(paths.Config), paths.Data} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("创建数据目录 %s: %w", dir, err)
		}
	}
	return nil
}
