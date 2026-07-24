package server

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	breakArmorCodexXSource     = "codex-x"
	breakArmorCodexXCatalogURL = "https://api.github.com/repos/yynxxxxx/Codex-X/contents/examples?ref=main"
	breakArmorCodexXRepoURL    = "https://github.com/yynxxxxx/Codex-X"
	breakArmorRemoteMaxBytes   = 128 * 1024
	breakArmorRemoteMaxEntries = 50
)

type breakArmorCodexXCatalogEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	SHA         string `json:"sha"`
	DownloadURL string `json:"download_url"`
}

func isReadOnlyBreakArmorTemplateID(id string) bool {
	id = strings.TrimSpace(id)
	return id == breakArmorTemplateV5 || id == breakArmorTemplateV35 || strings.HasPrefix(id, breakArmorCodexXSource+":")
}

func codexXTemplateID(name string) string {
	return breakArmorCodexXSource + ":" + strings.ToLower(strings.TrimSpace(name))
}

func codexXTemplateAllowlist() map[string]struct{} {
	allowed := make(map[string]struct{}, len(breakArmorCodexXBundledCatalog))
	for _, descriptor := range breakArmorCodexXBundledCatalog {
		allowed[descriptor.FileName] = struct{}{}
	}
	return allowed
}

func gitBlobSHA(raw []byte) string {
	hash := sha1.New() // #nosec G505 -- GitHub's contents API identifies Git blobs with SHA-1.
	_, _ = fmt.Fprintf(hash, "blob %d\x00", len(raw))
	_, _ = hash.Write(raw)
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func validateCodexXDownloadURL(rawURL, name string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.New("Codex-X 模板下载地址无效")
	}
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "raw.githubusercontent.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("Codex-X 模板下载地址不受信任")
	}
	expectedPath := "/yynxxxxx/Codex-X/main/examples/" + name
	if parsed.Path != expectedPath {
		return errors.New("Codex-X 模板下载地址超出参考目录")
	}
	return nil
}

func fetchBreakArmorHTTPBody(ctx context.Context, client *http.Client, rawURL string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "vision-relay/break-armor-template-sync")
	requestClient := *client
	requestClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := requestClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Codex-X 返回 %d：%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("Codex-X 响应超过 %d KB 限制", maxBytes/1024)
	}
	return raw, nil
}

func fetchBreakArmorCodexXTemplates(ctx context.Context, client *http.Client, targetClient string) ([]breakArmorSavedTemplate, error) {
	if client == nil {
		client = http.DefaultClient
	}
	catalogRaw, err := fetchBreakArmorHTTPBody(ctx, client, breakArmorCodexXCatalogURL, 1024*1024)
	if err != nil {
		return nil, fmt.Errorf("读取 Codex-X 模板目录失败：%w", err)
	}
	var catalog []breakArmorCodexXCatalogEntry
	if err := json.Unmarshal(catalogRaw, &catalog); err != nil {
		return nil, errors.New("Codex-X 模板目录格式无效")
	}
	if len(catalog) > breakArmorRemoteMaxEntries {
		return nil, fmt.Errorf("Codex-X 模板目录超过 %d 项限制", breakArmorRemoteMaxEntries)
	}
	sort.Slice(catalog, func(i, j int) bool { return strings.ToLower(catalog[i].Name) < strings.ToLower(catalog[j].Name) })
	allowed := codexXTemplateAllowlist()
	candidates := make([]breakArmorCodexXCatalogEntry, 0, len(allowed))
	seenIDs := make(map[string]string, len(allowed))
	for _, entry := range catalog {
		name := strings.TrimSpace(entry.Name)
		if entry.Type != "file" || !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		if _, ok := allowed[name]; !ok {
			continue
		}
		if name == "" || path.Base(name) != name || entry.Path != "examples/"+name {
			return nil, errors.New("Codex-X 模板目录包含无效路径")
		}
		id := codexXTemplateID(name)
		if previous, ok := seenIDs[id]; ok {
			return nil, fmt.Errorf("Codex-X 模板目录包含重复 ID：%s 与 %s", previous, name)
		}
		seenIDs[id] = name
		if err := validateCodexXDownloadURL(entry.DownloadURL, name); err != nil {
			return nil, err
		}
		entry.Name = name
		candidates = append(candidates, entry)
	}
	if len(candidates) == 0 {
		return nil, errors.New("Codex-X 模板目录没有可同步的已支持模板")
	}
	items := make([]breakArmorSavedTemplate, 0, len(candidates))
	now := time.Now().UTC()
	for _, entry := range candidates {
		name := entry.Name
		promptRaw, err := fetchBreakArmorHTTPBody(ctx, client, entry.DownloadURL, breakArmorRemoteMaxBytes)
		if err != nil {
			return nil, fmt.Errorf("同步 %s 失败：%w", name, err)
		}
		wantSHA := strings.ToLower(strings.TrimSpace(entry.SHA))
		if gotSHA := gitBlobSHA(promptRaw); wantSHA == "" || gotSHA != wantSHA {
			return nil, fmt.Errorf("Codex-X 模板 %s 内容与目录版本不一致", name)
		}
		prompt := strings.TrimSpace(string(promptRaw))
		if prompt == "" {
			return nil, fmt.Errorf("Codex-X 模板 %s 为空", name)
		}
		items = append(items, breakArmorSavedTemplate{
			ID:             codexXTemplateID(name),
			Client:         targetClient,
			Name:           name,
			Description:    codexXTemplateDescription(name),
			Prompt:         prompt,
			ReadOnly:       true,
			Source:         breakArmorCodexXSource,
			SourceURL:      codexXTemplateSourceURL(name),
			SourceRevision: strings.TrimSpace(entry.SHA),
			UpdatedAt:      now,
		})
	}
	return items, nil
}

func replaceBreakArmorCodexXTemplates(home, client string, items []breakArmorSavedTemplate) ([]breakArmorSavedTemplate, error) {
	breakArmorTemplatesMu.Lock()
	defer breakArmorTemplatesMu.Unlock()
	store, err := loadBreakArmorTemplatesUnlocked(home)
	if err != nil {
		return nil, err
	}
	existingByID := make(map[string]breakArmorSavedTemplate)
	out := make([]breakArmorSavedTemplate, 0, len(store.Templates)+len(items))
	for _, item := range store.Templates {
		if item.Client == client && item.Source == breakArmorCodexXSource {
			existingByID[item.ID] = item
			continue
		}
		out = append(out, item)
	}
	bundledByID := make(map[string]breakArmorSavedTemplate)
	for _, item := range bundledBreakArmorCodexXTemplates(client) {
		bundledByID[item.ID] = item
	}
	changed := make([]breakArmorSavedTemplate, 0, len(items))
	for _, item := range items {
		item.Client = client
		item.Builtin = false
		item.Bundled = false
		item.ReadOnly = true
		item.Source = breakArmorCodexXSource
		existing, hasExisting := existingByID[item.ID]
		bundled, hasBundled := bundledByID[item.ID]
		effectivePrompt := ""
		hasEffective := false
		if hasExisting {
			effectivePrompt, hasEffective = existing.Prompt, true
		} else if hasBundled {
			effectivePrompt, hasEffective = bundled.Prompt, true
		}
		if !hasEffective || effectivePrompt != item.Prompt {
			changed = append(changed, item)
		}
		if hasBundled && bundled.Prompt == item.Prompt {
			continue
		}
		if hasExisting && existing.Prompt == item.Prompt {
			item.UpdatedAt = existing.UpdatedAt
		}
		out = append(out, item)
	}
	unchanged := len(store.Templates) == len(out)
	if unchanged {
		for index := range out {
			if store.Templates[index] != out[index] {
				unchanged = false
				break
			}
		}
	}
	if unchanged {
		return changed, nil
	}
	store.Templates = out
	if err := saveBreakArmorTemplatesUnlocked(home, store); err != nil {
		return nil, err
	}
	return changed, nil
}

func syncBreakArmorCodexXTemplates(ctx context.Context, client *http.Client, home, targetClient string) ([]breakArmorSavedTemplate, error) {
	items, err := fetchBreakArmorCodexXTemplates(ctx, client, targetClient)
	if err != nil {
		return nil, err
	}
	changed, err := replaceBreakArmorCodexXTemplates(home, targetClient, items)
	if err != nil {
		return nil, fmt.Errorf("保存 Codex-X 模板失败：%w", err)
	}
	return changed, nil
}
