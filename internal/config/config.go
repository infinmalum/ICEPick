package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
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
	Model      Model       `json:"model"`
	MCPServers []MCPServer `json:"mcpServers,omitempty"`
}

// MCPServer describes an explicitly enabled stdio MCP server process.
type MCPServer struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type Model struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Name     string `json:"name"`
}

func Default() Config {
	return Config{Model: DefaultModel("ollama")}
}

func DefaultModel(provider string) Model {
	if provider == "lmstudio" {
		return Model{Provider: provider, BaseURL: "http://127.0.0.1:1234/v1"}
	}
	return Model{Provider: "ollama", BaseURL: "http://127.0.0.1:11434/v1"}
}

func (m Model) Validate() error {
	if m.Provider != "ollama" && m.Provider != "lmstudio" {
		return fmt.Errorf("暂不支持模型提供方 %q", m.Provider)
	}
	u, err := url.Parse(m.BaseURL)
	if err != nil || u.Scheme != "http" || u.User != nil || strings.ContainsAny(m.BaseURL, "?#") || strings.TrimRight(u.Path, "/") != "/v1" {
		return errors.New("模型地址必须是本机 HTTP 服务的 /v1 地址")
	}
	host := u.Hostname()
	if host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback()) {
		return errors.New("模型地址必须指向本机")
	}
	if u.Port() == "" {
		return errors.New("模型地址必须包含端口")
	}
	return nil
}

// Load reads a user-level config. A missing file gets safe local defaults.
func Load(path string) (Config, error) {
	result := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("读取配置: %w", err)
	}
	var saved Config
	if err := json.Unmarshal(data, &saved); err != nil {
		return Config{}, fmt.Errorf("解析配置: %w", err)
	}
	if saved.Model.Provider != "" {
		result.Model = DefaultModel(saved.Model.Provider)
		result.Model.Provider = saved.Model.Provider
	}
	if saved.Model.BaseURL != "" {
		result.Model.BaseURL = saved.Model.BaseURL
	}
	result.Model.Name = saved.Model.Name
	result.MCPServers = saved.MCPServers
	if err := result.Model.Validate(); err != nil {
		return Config{}, err
	}
	return result, nil
}

func Save(path string, cfg Config) error {
	if err := cfg.Model.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func EnsureDirs(paths Paths) error {
	for _, dir := range []string{filepath.Dir(paths.Config), paths.Data} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("创建数据目录 %s: %w", dir, err)
		}
	}
	return nil
}
