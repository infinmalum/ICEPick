package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CredentialLookup resolves the API key stored for a remote provider. It is
// the only bridge between the model adapters and the credential store; keys
// never reach config files, profiles, runs or logs.
type CredentialLookup func(provider string) (string, bool)

// Credentials is the on-disk credential store for remote model providers. It
// lives in the protected data directory (0600 file, 0700 directory), keyed by
// provider name. Local providers are refused — they stay key-less.
type Credentials struct {
	Providers map[string]string `json:"providers"`
}

// LoadCredentials reads the credential store. A missing file yields an empty
// store; a malformed one is an error so corruption is visible.
func LoadCredentials(path string) (Credentials, error) {
	result := Credentials{Providers: map[string]string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("读取凭据: %w", err)
	}
	var saved Credentials
	if err := json.Unmarshal(data, &saved); err != nil {
		return Credentials{}, fmt.Errorf("解析凭据: %w", err)
	}
	for provider, key := range saved.Providers {
		if !IsRemoteProvider(provider) {
			return Credentials{}, fmt.Errorf("凭据文件包含未知提供方 %q", provider)
		}
		result.Providers[provider] = key
	}
	return result, nil
}

// SaveCredentials atomically replaces the credential store with 0600
// permissions, mirroring the config file's write path.
func SaveCredentials(path string, creds Credentials) error {
	if creds.Providers == nil {
		creds.Providers = map[string]string{}
	}
	for provider := range creds.Providers {
		if !IsRemoteProvider(provider) {
			return fmt.Errorf("不支持为 %q 保存凭据（本地模型无需密钥）", provider)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credentials-*")
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

// Key returns the stored API key for one provider.
func (c Credentials) Key(provider string) (string, bool) {
	key, ok := c.Providers[provider]
	if !ok || strings.TrimSpace(key) == "" {
		return "", false
	}
	return key, true
}

// SetCredential validates and stores one provider's API key.
func (c *Credentials) SetCredential(provider, apiKey string) error {
	if !IsRemoteProvider(provider) {
		return fmt.Errorf("不支持为 %q 保存凭据（本地模型无需密钥，未知提供方）", provider)
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return errors.New("API 密钥不能为空")
	}
	if len(apiKey) > 4096 {
		return errors.New("API 密钥过长")
	}
	if c.Providers == nil {
		c.Providers = map[string]string{}
	}
	c.Providers[provider] = apiKey
	return nil
}

// DeleteCredential removes one provider's stored key.
func (c *Credentials) DeleteCredential(provider string) error {
	if !IsRemoteProvider(provider) {
		return fmt.Errorf("不支持删除 %q 的凭据", provider)
	}
	delete(c.Providers, provider)
	return nil
}

// FileCredentialLookup returns a CredentialLookup that reads the store from
// disk on every resolution, so saved or removed keys apply without a restart.
// Load failures resolve to "no key" — the adapters then fail with the
// explicit "credential missing" error instead of leaking the cause.
func FileCredentialLookup(path string) CredentialLookup {
	return func(provider string) (string, bool) {
		creds, err := LoadCredentials(path)
		if err != nil {
			return "", false
		}
		return creds.Key(provider)
	}
}
