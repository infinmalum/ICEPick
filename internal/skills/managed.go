package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"gopkg.in/yaml.v3"
	"strings"
	"unicode/utf8"
)

// Managed skills are immutable content-backed copies, persisted by the host.
// They use the same metadata, lazy loading and frozen snapshots as files.
func ParseSnapshot(content string) (Snapshot, error) {
	if len(content) > MaxSnapshotContent || !utf8.ValidString(content) {
		return Snapshot{}, errors.New("Skill 必须为UTF-8，且不超过256KiB")
	}
	meta, body, err := parse([]byte(content))
	if err != nil {
		return Snapshot{}, err
	}
	if strings.TrimSpace(body) == "" {
		return Snapshot{}, errors.New("Skill 正文为空")
	}
	hash := sha256.Sum256([]byte(content))
	return Snapshot{Name: meta.Name, Description: meta.Description, Content: content, SHA256: hex.EncodeToString(hash[:])}, nil
}
func CopySnapshot(content, suffix string) (Snapshot, error) {
	snap, err := ParseSnapshot(content)
	if err != nil {
		return Snapshot{}, err
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	parts := strings.SplitN(normalized[4:], "\n---\n", 2)
	var header map[string]any
	if err = yaml.Unmarshal([]byte(parts[0]), &header); err != nil {
		return Snapshot{}, err
	}
	prefix := snap.Name
	if len(prefix) > 50 {
		prefix = strings.TrimRight(prefix[:50], "-")
	}
	header["name"] = prefix + "-" + suffix
	raw, err := yaml.Marshal(header)
	if err != nil {
		return Snapshot{}, err
	}
	return ParseSnapshot("---\n" + string(raw) + "---\n" + parts[1])
}
func (c *Catalog) ManagedSnapshot(name string) (Snapshot, bool) {
	if c == nil {
		return Snapshot{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.managed[name]
	s.Resources = CloneResources(s.Resources)
	s.Scopes = append([]string(nil), s.Scopes...)
	return s, ok
}
func (c *Catalog) RegisterManaged(snap Snapshot) error {
	checked, err := ParseSnapshot(snap.Content)
	if err != nil {
		return err
	}
	if checked.Name != snap.Name || checked.SHA256 != snap.SHA256 {
		return errors.New("已发布Skill指纹不匹配")
	}
	if err := ValidateResources(snap.Resources); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.items[snap.Name]; ok {
		if prior, managed := c.managed[old.Name]; managed && prior.Fingerprint() == snap.Fingerprint() {
			return nil
		}
		return errors.New("已发布Skill名称与现有资源冲突")
	}
	if c.items == nil {
		c.items = map[string]Skill{}
	}
	if c.managed == nil {
		c.managed = map[string]Snapshot{}
	}
	checked.Resources = CloneResources(snap.Resources)
	checked.Scopes = append([]string(nil), snap.Scopes...)
	c.managed[snap.Name] = checked
	c.items[snap.Name] = Skill{Name: snap.Name, Description: snap.Description}
	return nil
}

func (c *Catalog) WithdrawManaged(name, hash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.managed[name]
	if !ok || item.SHA256 != hash {
		return errors.New("已发布Skill已变化或不可撤销")
	}
	delete(c.managed, name)
	delete(c.items, name)
	return nil
}
