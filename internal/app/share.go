package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"icepick/internal/config"
	"icepick/internal/storage"
)

// profile share packages: a Profile exports as a zip (or a directory with
// the same layout) that carries only the shareable combination — manifest,
// profile references, the persona and prompt as standalone text files, and
// the standard Skills directories. Local identity, machine policy, sessions,
// memories, credentials and approvals never enter the package.
//
// Import creates a new Profile with a fresh ID; it never auto-installs
// skills, never creates server configs, never creates or switches an Context
// and never activates authorization. The preview lists missing dependencies
// and the recommendations the receiver must act on explicitly.

const shareFormatVersion = 6

// Package size caps: a share bundle is metadata plus text assets.
const (
	shareMaxFileSize  = 16 << 20
	shareMaxTotalSize = 64 << 20
	shareMaxFiles     = 2000
)

var shareProfileFileName = "profile.json"
var shareManifestFileName = "manifest.json"

type shareManifest struct {
	FormatVersion int    `json:"formatVersion"`
	Generator     string `json:"generator"`
	ExportedAt    string `json:"exportedAt"`
	Name          string `json:"name"`
}

// shareProfile is the transport shape of a Profile combination. Persona and
// prompt live in standalone text files next to it.
type shareProfile struct {
	SchemaVersion     int               `json:"schemaVersion"`
	Execution         storage.Execution `json:"execution"`
	Description       string            `json:"description"`
	Model             config.Model      `json:"model"`
	AllMCPServers     bool              `json:"allMcpServers"`
	EnabledMCPServers []string          `json:"enabledMcpServers,omitempty"`
	MCPServerRefs     []shareMCPServer  `json:"mcpServerRefs,omitempty"`
	AllowedTools      []string          `json:"allowedTools,omitempty"`
	AllSkills         bool              `json:"allSkills"`
	EnabledSkills     []string          `json:"enabledSkills,omitempty"`
	Delegation        shareDelegation   `json:"delegation"`
	ContextBudget     int               `json:"contextBudget"`
	MemoryEnabled     bool              `json:"memoryEnabled"`
	PlanEnabled       bool              `json:"planEnabled"`
}

// shareMCPServer is a dependency reference, not a config: the receiver
// re-creates the server entry themselves. Absolute commands and working
// directories never leave the machine — the command is reduced to its base
// name and the adjustment is flagged.
type shareMCPServer struct {
	Name            string   `json:"name"`
	Command         string   `json:"command"`
	Args            []string `json:"args,omitempty"`
	Env             []string `json:"env,omitempty"`
	CommandAdjusted bool     `json:"commandAdjusted,omitempty"`
}

// shareDelegation carries the worker execution limits.
type shareDelegation struct {
	Enabled        bool `json:"enabled"`
	MaxDepth       int  `json:"maxDepth"`
	MaxConcurrency int  `json:"maxConcurrency"`
	MaxSeconds     int  `json:"maxSeconds"`
}

type ExportResult struct {
	Filename string `json:"filename"`
	// Data is the zip package, base64-encoded for the binding/IPC layers.
	Data string `json:"data"`
}

// ImportResult bundles what an import created with the same dependency
// preview the confirmation step showed.
type ImportResult struct {
	Profile storage.Profile `json:"profile"`
	Preview ImportPreview   `json:"preview"`
}

// ExportProfile bundles one profile into a zip package. Nothing instance-
// specific enters the bundle: the export reads only the profile content,
// the MCP config references and the installed skill directories.
func (s *Service) ExportProfile(profileID string) (ExportResult, error) {
	return s.exportProfile(profileID, false)
}

func (s *Service) ExportProfileIncludingWorkers(profileID string) (ExportResult, error) {
	return s.exportProfile(profileID, true)
}

func (s *Service) exportProfile(profileID string, includeWorkers bool) (ExportResult, error) {
	if err := s.checkReady(); err != nil {
		return ExportResult{}, err
	}
	profile, err := s.store.GetProfile(context.Background(), profileID)
	if err != nil {
		return ExportResult{}, err
	}
	for i, source := range profile.Data.Execution.WorkerSources {
		if source.ProfileID == "" {
			continue
		}
		if !includeWorkers {
			return ExportResult{}, errors.New("此 Profile 引用了其他 worker 配置，请选择包含 worker 配置导出")
		}
		dependency, err := s.store.GetProfile(context.Background(), source.ProfileID)
		if err != nil {
			return ExportResult{}, err
		}
		portable, err := storage.WorkerConfigurationFrom(dependency)
		if err != nil {
			return ExportResult{}, err
		}
		profile.Data.Execution.WorkerSources[i].ProfileID = ""
		profile.Data.Execution.WorkerSources[i].Configuration = &portable
	}
	files := map[string][]byte{}
	manifest := shareManifest{
		FormatVersion: shareFormatVersion,
		Generator:     "ICEPick",
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		Name:          profile.Name,
	}
	if err := marshalShareFile(files, shareManifestFileName, manifest); err != nil {
		return ExportResult{}, err
	}
	share := shareProfile{
		SchemaVersion:     profile.Data.SchemaVersion,
		Execution:         profile.Data.Execution,
		Description:       profile.Data.Description,
		Model:             profile.Data.Model,
		AllMCPServers:     profile.Data.AllMCPServers,
		EnabledMCPServers: profile.Data.EnabledMCPServers,
		AllowedTools:      profile.Data.AllowedTools,
		AllSkills:         profile.Data.AllSkills,
		EnabledSkills:     profile.Data.EnabledSkills,
		Delegation: shareDelegation{
			Enabled:        profile.Data.Delegation.Enabled,
			MaxDepth:       profile.Data.Delegation.MaxDepth,
			MaxConcurrency: profile.Data.Delegation.MaxConcurrency,
			MaxSeconds:     profile.Data.Delegation.MaxSeconds,
		},
		ContextBudget: profile.Data.ContextBudget,
		MemoryEnabled: profile.Data.MemoryEnabled,
		PlanEnabled:   profile.Data.PlanEnabled,
	}
	if profile.Data.Persona != "" {
		files["persona.md"] = []byte(profile.Data.Persona)
	}
	if profile.Data.SystemPrompt != "" {
		files["prompt.md"] = []byte(profile.Data.SystemPrompt)
	}
	// MCP references: only for servers the profile actually selects. The
	// command is reduced to a base name; working directories and anything
	// path-shaped stay home.
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()
	configured := map[string]config.MCPServer{}
	for _, server := range cfg.MCPServers {
		configured[server.Name] = server
	}
	referenced := profile.Data.EnabledMCPServers
	if profile.Data.AllMCPServers {
		referenced = nil
		for name := range configured {
			referenced = append(referenced, name)
		}
		sort.Strings(referenced)
	}
	for _, name := range referenced {
		server, ok := configured[name]
		if !ok {
			continue
		}
		command := server.Command
		adjusted := false
		if filepath.IsAbs(command) || strings.ContainsRune(command, filepath.Separator) || strings.ContainsRune(command, '/') {
			command = filepath.Base(command)
			adjusted = true
		}
		args := make([]string, 0, len(server.Args))
		for _, arg := range server.Args {
			if filepath.IsAbs(arg) {
				adjusted = true
				continue
			}
			args = append(args, arg)
		}
		share.MCPServerRefs = append(share.MCPServerRefs, shareMCPServer{
			Name: server.Name, Command: command, Args: args, Env: server.Env, CommandAdjusted: adjusted,
		})
	}
	if err := marshalShareFile(files, shareProfileFileName, share); err != nil {
		return ExportResult{}, err
	}
	// Skill bundles: every installed skill the profile selects.
	selected := profile.Data.EnabledSkills
	if profile.Data.AllSkills {
		selected = nil
		for _, item := range s.skills.Items() {
			selected = append(selected, item.Name)
		}
	}
	for _, name := range selected {
		item := s.skills.Item(name)
		if item == nil {
			continue
		}
		root := filepath.Dir(item.Path)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !entry.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files["skills/"+name+"/"+filepath.ToSlash(rel)] = data
			return nil
		})
		if err != nil {
			return ExportResult{}, fmt.Errorf("打包 Skill %s: %w", name, err)
		}
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		writer, err := zw.Create(name)
		if err != nil {
			return ExportResult{}, err
		}
		if _, err := writer.Write(files[name]); err != nil {
			return ExportResult{}, err
		}
	}
	if err := zw.Close(); err != nil {
		return ExportResult{}, err
	}
	return ExportResult{Filename: shareFileName(profile.Name) + ".zip", Data: base64.StdEncoding.EncodeToString(buf.Bytes())}, nil
}

func shareFileName(name string) string {
	replaced := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', ' ':
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if replaced == "" {
		return "icepick-profile"
	}
	return replaced
}

func marshalShareFile(files map[string][]byte, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	files[name] = data
	return nil
}

// ImportSkill is one skill dependency in the import preview: whether the
// package bundles it and whether this host already installed it. Importing
// never installs anything.
type ImportSkill struct {
	Name      string `json:"name"`
	Bundled   bool   `json:"bundled"`
	Installed bool   `json:"installed"`
}

// ImportMCPServer is one server dependency in the import preview.
type ImportMCPServer struct {
	Name            string `json:"name"`
	Command         string `json:"command"`
	CommandAdjusted bool   `json:"commandAdjusted"`
	Configured      bool   `json:"configured"`
}

// ImportPreview is everything the receiver should see before confirming an
// import: the combination, its dependencies and the explicit recommendations.
type ImportWorker struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Bundled bool   `json:"bundled"`
}

type ImportPreview struct {
	Workers       []ImportWorker           `json:"workers"`
	Name          string                   `json:"name"`
	Description   string                   `json:"description"`
	Model         config.Model             `json:"model"`
	PersonaChars  int                      `json:"personaChars"`
	PromptChars   int                      `json:"promptChars"`
	ContextBudget int                      `json:"contextBudget"`
	Delegation    storage.DelegationPolicy `json:"delegation"`
	MemoryEnabled bool                     `json:"memoryEnabled"`
	PlanEnabled   bool                     `json:"planEnabled"`
	MCPServers    []ImportMCPServer        `json:"mcpServers"`
	Skills        []ImportSkill            `json:"skills"`
	Warnings      []string                 `json:"warnings"`
}

type sharePackage struct {
	manifest shareManifest
	profile  shareProfile
	persona  string
	prompt   string
	skills   []string // bundled skill names (skills/<name>/ prefix)
	nameSet  []string // every file name in the package
}

// readShareFiles turns a zip archive into an in-memory file map with strict
// bounds: regular files only, no path escapes, size caps per file and total.
func readShareFiles(data []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("无法读取分享包: %w", err)
	}
	if len(reader.File) > shareMaxFiles {
		return nil, fmt.Errorf("分享包文件数超过 %d 上限", shareMaxFiles)
	}
	files := map[string][]byte{}
	total := 0
	for _, file := range reader.File {
		name := file.Name
		if file.Mode()&os.ModeSymlink != 0 || !file.Mode().IsRegular() && !strings.HasSuffix(name, "/") {
			return nil, fmt.Errorf("分享包含非常规文件 %q", name)
		}
		if strings.HasSuffix(name, "/") {
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(name))
		if strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") || clean == ".." || strings.Contains(clean, "\x00") {
			return nil, fmt.Errorf("分享包路径越界: %q", name)
		}
		if !fs.ValidPath(clean) {
			return nil, fmt.Errorf("分享包路径无效: %q", name)
		}
		size := int64(file.UncompressedSize64)
		if size > shareMaxFileSize {
			return nil, fmt.Errorf("分享包内文件 %s 超过大小上限", name)
		}
		total += int(size)
		if total > shareMaxTotalSize {
			return nil, errors.New("分享包总大小超过上限")
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(rc, shareMaxFileSize+1))
		rc.Close()
		if err != nil {
			return nil, err
		}
		if len(content) > shareMaxFileSize {
			return nil, fmt.Errorf("分享包内文件 %s 超过大小上限", name)
		}
		files[clean] = content
	}
	return files, nil
}

// parseSharePackage validates the structure and assembles the transported
// profile content.
func parseSharePackage(files map[string][]byte) (*sharePackage, error) {
	rawManifest, ok := files[shareManifestFileName]
	if !ok {
		return nil, errors.New("分享包缺少 manifest.json")
	}
	var manifest shareManifest
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		return nil, fmt.Errorf("解析 manifest.json: %w", err)
	}
	if manifest.FormatVersion != shareFormatVersion {
		return nil, fmt.Errorf("分享包版本不兼容（格式 %d，本程序支持 %d）", manifest.FormatVersion, shareFormatVersion)
	}
	rawProfile, ok := files[shareProfileFileName]
	if !ok {
		return nil, errors.New("分享包缺少 profile.json")
	}
	var share shareProfile
	if err := json.Unmarshal(rawProfile, &share); err != nil {
		return nil, fmt.Errorf("解析 profile.json: %w", err)
	}
	for _, source := range share.Execution.WorkerSources {
		if source.ProfileID != "" {
			return nil, errors.New("分享包不能绑定本机 Profile ID；请导出独立 worker 配置")
		}
	}
	pkg := &sharePackage{manifest: manifest, profile: share}
	if data, ok := files["persona.md"]; ok {
		pkg.persona = string(data)
	}
	if data, ok := files["prompt.md"]; ok {
		pkg.prompt = string(data)
	}
	for name := range files {
		pkg.nameSet = append(pkg.nameSet, name)
		if strings.HasPrefix(name, "skills/") {
			rest := strings.TrimPrefix(name, "skills/")
			if skill := strings.SplitN(rest, "/", 2)[0]; skill != "" && !contains(pkg.skills, skill) {
				pkg.skills = append(pkg.skills, skill)
			}
		}
	}
	sort.Strings(pkg.skills)
	sort.Strings(pkg.nameSet)
	return pkg, nil
}

// toProfileData assembles the stored shape.
func (pkg *sharePackage) toProfileData() (storage.ProfileData, error) {
	data := storage.ProfileData{
		SchemaVersion:     pkg.profile.SchemaVersion,
		Execution:         pkg.profile.Execution,
		Description:       pkg.profile.Description,
		Persona:           pkg.persona,
		SystemPrompt:      pkg.prompt,
		Model:             pkg.profile.Model,
		AllMCPServers:     pkg.profile.AllMCPServers,
		EnabledMCPServers: append([]string(nil), pkg.profile.EnabledMCPServers...),
		AllowedTools:      append([]string(nil), pkg.profile.AllowedTools...),
		AllSkills:         pkg.profile.AllSkills,
		EnabledSkills:     append([]string(nil), pkg.profile.EnabledSkills...),
		Delegation: storage.DelegationPolicy{
			Enabled:        pkg.profile.Delegation.Enabled,
			MaxDepth:       pkg.profile.Delegation.MaxDepth,
			MaxConcurrency: pkg.profile.Delegation.MaxConcurrency,
			MaxSeconds:     pkg.profile.Delegation.MaxSeconds,
		},
		ContextBudget: pkg.profile.ContextBudget,
		MemoryEnabled: pkg.profile.MemoryEnabled,
		PlanEnabled:   pkg.profile.PlanEnabled,
	}
	if data.ContextBudget == 0 {
		data.ContextBudget = storage.BlankProfile().ContextBudget
	}
	if len(data.AllowedTools) == 0 {
		data.AllowedTools = []string{}
	}
	if len(data.EnabledSkills) == 0 {
		data.EnabledSkills = []string{}
	}
	if len(data.EnabledMCPServers) == 0 {
		data.EnabledMCPServers = []string{}
	}
	if err := storage.ValidateProfileData(data); err != nil {
		return storage.ProfileData{}, err
	}
	return data, nil
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// buildImportPreview compares the package against what this host offers.
func (s *Service) buildImportPreview(pkg *sharePackage) ImportPreview {
	data, err := pkg.toProfileData()
	preview := ImportPreview{
		Name:          pkg.manifest.Name,
		Description:   pkg.profile.Description,
		Model:         pkg.profile.Model,
		PersonaChars:  len(pkg.persona),
		PromptChars:   len(pkg.prompt),
		ContextBudget: data.ContextBudget,
		Delegation:    data.Delegation,
		MemoryEnabled: data.MemoryEnabled,
		PlanEnabled:   data.PlanEnabled,
		Warnings:      []string{},
	}
	if err != nil {
		preview.Warnings = append(preview.Warnings, "Profile 内容无效: "+err.Error())
		return preview
	}
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()
	configured := map[string]config.MCPServer{}
	for _, server := range cfg.MCPServers {
		configured[server.Name] = server
	}
	referenced := pkg.profile.EnabledMCPServers
	if pkg.profile.AllMCPServers {
		preview.Warnings = append(preview.Warnings, "该 Profile 声明“使用全部 MCP 服务”：导入后它将引用接收方本机接入的全部服务，请按需收紧")
		referenced = nil
		for name := range configured {
			referenced = append(referenced, name)
		}
		sort.Strings(referenced)
	}
	for _, ref := range pkg.profile.MCPServerRefs {
		server, ok := configured[ref.Name]
		preview.MCPServers = append(preview.MCPServers, ImportMCPServer{
			Name: ref.Name, Command: ref.Command, CommandAdjusted: ref.CommandAdjusted, Configured: ok,
		})
		if !ok {
			preview.Warnings = append(preview.Warnings, fmt.Sprintf("MCP 服务 %q 未接入本机（包内引用命令 %q），导入后需自行配置该服务", ref.Name, ref.Command))
		} else if ref.CommandAdjusted {
			preview.Warnings = append(preview.Warnings, fmt.Sprintf("MCP 服务 %q 的命令在导出时去除了本机路径，导入后请核对本机命令（当前 %q）", ref.Name, server.Command))
		}
	}
	for _, name := range referenced {
		if !contains(pkg.profile.EnabledMCPServers, name) && !pkg.profile.AllMCPServers {
			continue
		}
		if _, ok := configured[name]; !ok {
			preview.MCPServers = append(preview.MCPServers, ImportMCPServer{Name: name, Configured: false})
			preview.Warnings = append(preview.Warnings, fmt.Sprintf("Profile 启用的 MCP 服务 %q 未接入本机，导入后需自行配置", name))
		}
	}
	bundled := map[string]bool{}
	for _, name := range pkg.skills {
		bundled[name] = true
		preview.Skills = append(preview.Skills, ImportSkill{Name: name, Bundled: true, Installed: s.skills.Installed(name)})
		if !s.skills.Installed(name) {
			preview.Warnings = append(preview.Warnings, fmt.Sprintf("Skill %q 已包含在包内但未安装到本机 Skills 目录；导入不会自动安装，请手动安装后该引用才可用", name))
		}
	}
	for _, name := range data.EnabledSkills {
		if bundled[name] {
			continue
		}
		installed := s.skills.Installed(name)
		preview.Skills = append(preview.Skills, ImportSkill{Name: name, Installed: installed})
		if !installed {
			preview.Warnings = append(preview.Warnings, fmt.Sprintf("Profile 启用的 Skill %q 不在包内也未安装到本机，导入后需手动安装", name))
		}
	}
	if data.MemoryEnabled {
		preview.Warnings = append(preview.Warnings, "包内的记忆偏好不会自动生效：仍需接收方本机上下文的记忆授权，且记忆数据从不随包传输")
	}
	if data.Delegation.Enabled {
		preview.Warnings = append(preview.Warnings, "worker 配置随 Profile 导入；本机资源权限需在对话中授权")
	}
	preview.Warnings = append(preview.Warnings, "导入只创建 Profile，不安装资源、不授权本机权限；可在对话中选择此 Profile。")
	for _, source := range data.Execution.WorkerSources {
		item := ImportWorker{Name: source.Name, Source: "当前 Profile 的 worker"}
		if source.Configuration != nil {
			item.Source = source.Configuration.Name
			item.Bundled = true
		}
		preview.Workers = append(preview.Workers, item)
	}
	return preview
}

// PreviewProfileImport parses a share package and reports what an import
// would create and which dependencies are missing.
func (s *Service) PreviewProfileImport(data []byte) (ImportPreview, error) {
	if err := s.checkReady(); err != nil {
		return ImportPreview{}, err
	}
	files, err := readShareFiles(data)
	if err != nil {
		return ImportPreview{}, err
	}
	pkg, err := parseSharePackage(files)
	if err != nil {
		return ImportPreview{}, err
	}
	return s.buildImportPreview(pkg), nil
}

// ImportProfile creates a new Profile from a share package. The new Profile
// gets a fresh ID at revision 1; when the name collides with an existing
// profile a suffix is appended (importing the same package twice yields two
// independent profiles). It runs the same content validation as an edit, but
// not the host resource check — missing dependencies are surfaced by the
// preview and resolved by the receiver.
func (s *Service) ImportProfile(data []byte) (storage.Profile, ImportPreview, error) {
	if err := s.checkReady(); err != nil {
		return storage.Profile{}, ImportPreview{}, err
	}
	files, err := readShareFiles(data)
	if err != nil {
		return storage.Profile{}, ImportPreview{}, err
	}
	pkg, err := parseSharePackage(files)
	if err != nil {
		return storage.Profile{}, ImportPreview{}, err
	}
	profileData, err := pkg.toProfileData()
	if err != nil {
		return storage.Profile{}, ImportPreview{}, err
	}
	name := strings.TrimSpace(pkg.manifest.Name)
	if name == "" {
		name = "导入的 Profile"
	}
	profiles, err := s.store.ListProfiles(context.Background())
	if err != nil {
		return storage.Profile{}, ImportPreview{}, err
	}
	taken := map[string]bool{}
	for _, profile := range profiles {
		taken[profile.Name] = true
	}
	if taken[name] {
		for i := 2; ; i++ {
			candidate := fmt.Sprintf("%s（导入 %d）", name, i)
			if !taken[candidate] {
				name = candidate
				break
			}
		}
	}
	profile, err := s.store.CreateProfile(context.Background(), name, profileData)
	if err != nil {
		return storage.Profile{}, ImportPreview{}, err
	}
	return profile, s.buildImportPreview(pkg), nil
}
