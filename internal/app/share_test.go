package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"icepick/internal/config"
	"icepick/internal/skills"
	"icepick/internal/storage"
)

func decodeBase64(t *testing.T, data string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func readFileFromZip(file *zip.File) (string, error) {
	rc, err := file.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	content, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// zipBytes builds an in-memory zip package for import tests.
func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		writer, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newShareService builds a service whose skills catalog contains the given
// skill directories, so export tests can observe bundling.
func newShareService(t *testing.T, skillFiles map[string]string) (*Service, *storage.Store) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range skillFiles {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := skills.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	svc, store := newTestService(t, &scriptModel{}, &scriptTools{respond: func(string, string) (string, error) { return "", nil }})
	svc.skills = catalog
	return svc, store
}

func sharePackageFiles(name string, data storage.ProfileData) map[string]string {
	persona, prompt := data.Persona, data.SystemPrompt
	data.Persona, data.SystemPrompt = "", ""
	profileJSON, _ := json.Marshal(map[string]any{
		"description": data.Description, "model": data.Model,
		"allMcpServers": data.AllMCPServers, "enabledMcpServers": data.EnabledMCPServers,
		"allowedTools": data.AllowedTools, "allSkills": data.AllSkills, "enabledSkills": data.EnabledSkills,
		"delegation":    map[string]any{"enabled": data.Delegation.Enabled, "maxDepth": data.Delegation.MaxDepth, "selection": "pinned", "targetAgentId": "some-local-agent"},
		"contextBudget": data.ContextBudget, "memoryEnabled": data.MemoryEnabled, "planEnabled": data.PlanEnabled,
	})
	files := map[string]string{
		"manifest.json": `{"formatVersion":1,"generator":"ICEPick","exportedAt":"2026-10-06T00:00:00Z","name":"` + name + `"}`,
		"profile.json":  string(profileJSON),
	}
	if persona != "" {
		files["persona.md"] = persona
	}
	if prompt != "" {
		files["prompt.md"] = prompt
	}
	return files
}

func TestProfileShareRoundTrip(t *testing.T) {
	svc, store := newShareService(t, nil)
	profile, err := svc.GetProfile(storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	profile.Data.Persona = "严谨的助手人格"
	profile.Data.SystemPrompt = "总是先列计划再执行"
	profile.Data.Delegation.Selection = storage.DelegationPinnedSelection
	profile.Data.Delegation.TargetAgentID = storage.DefaultAgentID
	if _, err := svc.UpdateProfile(profile); err != nil {
		t.Fatal(err)
	}
	exported, err := svc.ExportProfile(storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(exported.Filename, ".zip") || exported.Data == "" {
		t.Fatalf("export result: %+v", exported)
	}
	preview, err := svc.PreviewProfileImport(decodeBase64(t, exported.Data))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Name != profile.Name || preview.PersonaChars != len(profile.Data.Persona) || preview.PromptChars != len(profile.Data.SystemPrompt) {
		t.Fatalf("preview mismatch: %+v", preview)
	}
	// The pinned target is a local identity: the preview must surface that
	// it was stripped.
	found := false
	for _, warning := range preview.Warnings {
		if strings.Contains(warning, "不携带目标") {
			found = true
		}
	}
	if !found {
		t.Fatalf("pinned-target warning missing: %+v", preview.Warnings)
	}
	imported, _, err := svc.ImportProfile(decodeBase64(t, exported.Data))
	if err != nil {
		t.Fatal(err)
	}
	if imported.ID == storage.DefaultProfileID || imported.Revision != 1 {
		t.Fatalf("imported profile identity: %+v", imported)
	}
	if imported.Data.Persona != profile.Data.Persona || imported.Data.SystemPrompt != profile.Data.SystemPrompt {
		t.Fatalf("imported content: %+v", imported.Data)
	}
	if imported.Data.Delegation.Selection != storage.DelegationModelSelection || imported.Data.Delegation.TargetAgentID != "" {
		t.Fatalf("pinned target leaked into import: %+v", imported.Data.Delegation)
	}
	// Importing the same package twice yields two independent profiles with
	// a disambiguated name.
	second, _, err := svc.ImportProfile(decodeBase64(t, exported.Data))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == imported.ID {
		t.Fatalf("duplicate import reused the profile id: %+v", second)
	}
	if second.Name == imported.Name {
		t.Fatalf("duplicate import kept the same name: %q", second.Name)
	}
	profiles, err := store.ListProfiles(context.Background())
	if err != nil || len(profiles) != 3 {
		t.Fatalf("profiles after double import: %+v %v", profiles, err)
	}
}

func TestProfileShareBundlesSkills(t *testing.T) {
	svc, _ := newShareService(t, map[string]string{
		"note-taker/SKILL.md": "---\nname: note-taker\ndescription: 记录笔记\n---\n正文内容",
	})
	profile, err := svc.GetProfile(storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	profile.Data.AllSkills = false
	profile.Data.EnabledSkills = []string{"note-taker"}
	if _, err := svc.UpdateProfile(profile); err != nil {
		t.Fatal(err)
	}
	exported, err := svc.ExportProfile(storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(decodeBase64(t, exported.Data)), int64(len(decodeBase64(t, exported.Data))))
	if err != nil {
		t.Fatal(err)
	}
	bundled := map[string]string{}
	for _, file := range reader.File {
		content, err := readFileFromZip(file)
		if err != nil {
			t.Fatal(err)
		}
		bundled[file.Name] = content
	}
	skill, ok := bundled["skills/note-taker/SKILL.md"]
	if !ok || !strings.Contains(skill, "正文内容") {
		t.Fatalf("skill not bundled: %v", bundled)
	}
	preview, err := svc.PreviewProfileImport(decodeBase64(t, exported.Data))
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Skills) != 1 || !preview.Skills[0].Bundled || !preview.Skills[0].Installed {
		t.Fatalf("skill preview: %+v", preview.Skills)
	}
}

// A package whose manifest version is newer than this binary understands
// must be refused, not silently misread.
func TestProfileShareRejectsIncompatibleVersion(t *testing.T) {
	svc, _ := newShareService(t, nil)
	data := zipBytes(t, map[string]string{
		"manifest.json": `{"formatVersion":99,"generator":"future","name":"x"}`,
		"profile.json":  `{}`,
	})
	if _, err := svc.PreviewProfileImport(data); err == nil || !strings.Contains(err.Error(), "版本不兼容") {
		t.Fatalf("version mismatch not refused: %v", err)
	}
	if _, _, err := svc.ImportProfile(data); err == nil {
		t.Fatal("import accepted an incompatible package")
	}
}

// Zip entries must never escape the package root.
func TestProfileShareRejectsPathEscape(t *testing.T) {
	svc, _ := newShareService(t, nil)
	data := zipBytes(t, map[string]string{
		"manifest.json":       `{"formatVersion":1,"name":"x"}`,
		"profile.json":        `{}`,
		"../escaped/evil.txt": "payload",
	})
	if _, err := svc.PreviewProfileImport(data); err == nil {
		t.Fatal("path escape not refused")
	}
}

// An import whose profile content is invalid is refused with a clear error.
func TestProfileShareRejectsInvalidContent(t *testing.T) {
	svc, _ := newShareService(t, nil)
	data := zipBytes(t, map[string]string{
		"manifest.json": `{"formatVersion":1,"name":"x"}`,
		"profile.json":  `{"contextBudget": 12}`,
	})
	if _, _, err := svc.ImportProfile(data); err == nil {
		t.Fatal("invalid profile content accepted")
	}
}

// MCP references lose absolute commands and working directories at export.
func TestProfileShareStripsLocalPaths(t *testing.T) {
	svc, _ := newShareService(t, nil)
	svc.mu.Lock()
	svc.config.MCPServers = []config.MCPServer{{
		Name: "fs", Command: "/opt/tools/fs-server", Args: []string{"--root", "/home/wux/secret"}, Env: []string{"DEBUG"},
	}}
	svc.mu.Unlock()
	profile, err := svc.GetProfile(storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	profile.Data.AllMCPServers = false
	profile.Data.EnabledMCPServers = []string{"fs"}
	if _, err := svc.UpdateProfile(profile); err != nil {
		t.Fatal(err)
	}
	exported, err := svc.ExportProfile(storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	blob := exported.Data
	raw, err := zip.NewReader(bytes.NewReader(decodeBase64(t, blob)), int64(len(decodeBase64(t, blob))))
	if err != nil {
		t.Fatal(err)
	}
	var profileJSON string
	for _, file := range raw.File {
		if file.Name == "profile.json" {
			content, err := readFileFromZip(file)
			if err != nil {
				t.Fatal(err)
			}
			profileJSON = content
		}
	}
	if strings.Contains(profileJSON, "/opt/tools") || strings.Contains(profileJSON, "/home/wux") {
		t.Fatalf("absolute paths leaked into package: %s", profileJSON)
	}
	if !strings.Contains(profileJSON, `"commandAdjusted": true`) || !strings.Contains(profileJSON, `"command": "fs-server"`) {
		t.Fatalf("command adjustment missing: %s", profileJSON)
	}
	preview, err := svc.PreviewProfileImport(decodeBase64(t, exported.Data))
	if err != nil {
		t.Fatal(err)
	}
	// On the same host the referenced server is configured; the preview
	// flags the stripped command for re-verification.
	if len(preview.MCPServers) != 1 || !preview.MCPServers[0].Configured || !preview.MCPServers[0].CommandAdjusted {
		t.Fatalf("mcp preview: %+v", preview.MCPServers)
	}
}
