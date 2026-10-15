package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Resource bytes belong to the same immutable run snapshot as SKILL.md.
type Resource struct {
	Data   []byte `json:"data"`
	SHA256 string `json:"sha256"`
}

const MaxResourceBytes = 1 << 20
const MaxResourceTotal = 8 << 20
const MaxResources = 128

func snapshotResources(root string) (map[string]Resource, int, error) {
	resources := map[string]Resource{}
	total := 0
	for _, dir := range []string{"references", "scripts", "assets"} {
		base := filepath.Join(root, dir)
		if _, err := os.Lstat(base); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(base, func(p string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("Skill 资源不允许符号链接: %s", p)
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("Skill 资源必须为普通文件: %s", p)
			}
			if len(resources) >= MaxResources {
				return fmt.Errorf("Skill 资源超过 %d 个", MaxResources)
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(f, MaxResourceBytes+1))
			f.Close()
			if err != nil {
				return err
			}
			total += len(data)
			if len(data) > MaxResourceBytes || total > MaxResourceTotal {
				return fmt.Errorf("Skill 资源超过单文件1MiB或总量8MiB上限")
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(data)
			resources[filepath.ToSlash(rel)] = Resource{Data: data, SHA256: hex.EncodeToString(hash[:])}
			return nil
		})
		if err != nil {
			return nil, 0, err
		}
	}
	return resources, total, nil
}
func (s Snapshot) ReadResource(name string) (string, error) {
	if name == "" || name == "SKILL.md" {
		names := make([]string, 0, len(s.Resources))
		for p := range s.Resources {
			names = append(names, p)
		}
		sort.Strings(names)
		if len(names) == 0 {
			return s.Content, nil
		}
		return s.Content + "\n\n[本任务冻结资源，可用 load_skill(name, path) 按需读取]\n" + strings.Join(names, "\n"), nil
	}
	if path.IsAbs(name) || path.Clean(name) != name || strings.Contains(name, "\\") || strings.HasPrefix(name, "../") {
		return "", fmt.Errorf("Skill 资源路径必须为清单中的相对路径")
	}
	r, ok := s.Resources[name]
	if !ok {
		return "", fmt.Errorf("资源 %q 不在 Skill 起始快照中", name)
	}
	if utf8.Valid(r.Data) && !strings.ContainsRune(string(r.Data), 0) {
		return string(r.Data), nil
	}
	raw, err := json.Marshal(map[string]any{"path": name, "encoding": "base64", "data": r.Data, "sha256": r.SHA256})
	return string(raw), err
}

// Fingerprint includes resource contents, so editing only a script invalidates a draft source.
func (s Snapshot) Fingerprint() string {
	raw, _ := json.Marshal(s)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func CloneResources(resources map[string]Resource) map[string]Resource {
	if resources == nil {
		return nil
	}
	copy := make(map[string]Resource, len(resources))
	for name, r := range resources {
		r.Data = append([]byte(nil), r.Data...)
		copy[name] = r
	}
	return copy
}
func ValidateResources(resources map[string]Resource) error {
	total := 0
	if len(resources) > MaxResources {
		return fmt.Errorf("Skill 资源过多")
	}
	for name, r := range resources {
		if path.Clean(name) != name || strings.Contains(name, "\\") || !(strings.HasPrefix(name, "references/") || strings.HasPrefix(name, "scripts/") || strings.HasPrefix(name, "assets/")) {
			return fmt.Errorf("Skill 资源路径无效")
		}
		total += len(r.Data)
		sum := sha256.Sum256(r.Data)
		if len(r.Data) > MaxResourceBytes || total > MaxResourceTotal || hex.EncodeToString(sum[:]) != r.SHA256 {
			return fmt.Errorf("Skill 资源超限或指纹不匹配")
		}
	}
	return nil
}
