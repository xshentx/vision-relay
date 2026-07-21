package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var breakArmorTemplatesMu sync.RWMutex

type breakArmorSavedTemplate struct {
	ID        string    `json:"id"`
	Client    string    `json:"client"`
	Name      string    `json:"name"`
	Prompt    string    `json:"prompt"`
	Builtin   bool      `json:"builtin"`
	UpdatedAt time.Time `json:"updated_at"`
}
type breakArmorTemplateStore struct {
	Version   int                       `json:"version"`
	Templates []breakArmorSavedTemplate `json:"templates"`
}
type breakArmorTemplateRequest struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Client string `json:"client"`
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
}

func breakArmorTemplatesPath(home string) string {
	return filepath.Join(home, ".vision-relay", "break-armor", "templates.json")
}
func builtinBreakArmorTemplates(client string) []breakArmorSavedTemplate {
	return []breakArmorSavedTemplate{{ID: "v5", Client: client, Name: "v5 稳定版", Prompt: strings.TrimSpace(breakArmorV5Prompt), Builtin: true}, {ID: "v35", Client: client, Name: "v35 特殊任务版", Prompt: strings.TrimSpace(breakArmorV35Prompt), Builtin: true}}
}
func loadBreakArmorTemplatesUnlocked(home string) (breakArmorTemplateStore, error) {
	raw, err := os.ReadFile(breakArmorTemplatesPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return breakArmorTemplateStore{Version: 1}, nil
	}
	if err != nil {
		return breakArmorTemplateStore{}, err
	}
	var store breakArmorTemplateStore
	if json.Unmarshal(raw, &store) != nil {
		return store, errors.New("破甲模板存储文件已损坏")
	}
	if store.Version == 0 {
		store.Version = 1
	}
	return store, nil
}

func loadBreakArmorTemplates(home string) (breakArmorTemplateStore, error) {
	breakArmorTemplatesMu.RLock()
	defer breakArmorTemplatesMu.RUnlock()
	return loadBreakArmorTemplatesUnlocked(home)
}

func saveBreakArmorTemplatesUnlocked(home string, store breakArmorTemplateStore) error {
	path := breakArmorTemplatesPath(home)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	store.Version = 1
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(dir, ".templates-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(raw)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func saveBreakArmorTemplates(home string, store breakArmorTemplateStore) error {
	breakArmorTemplatesMu.Lock()
	defer breakArmorTemplatesMu.Unlock()
	return saveBreakArmorTemplatesUnlocked(home, store)
}

func listBreakArmorTemplates(home, client string) ([]breakArmorSavedTemplate, error) {
	client = normalizeBreakArmorClient(client)
	if client == "" {
		client = breakArmorClientCodex
	}
	breakArmorTemplatesMu.RLock()
	defer breakArmorTemplatesMu.RUnlock()
	store, err := loadBreakArmorTemplatesUnlocked(home)
	if err != nil {
		return nil, err
	}
	out := builtinBreakArmorTemplates(client)
	for _, item := range store.Templates {
		if item.Client == client {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Builtin != out[j].Builtin {
			return out[i].Builtin
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func upsertBreakArmorTemplate(home string, req breakArmorTemplateRequest) (breakArmorSavedTemplate, error) {
	client := normalizeBreakArmorClient(req.Client)
	if client == "" {
		return breakArmorSavedTemplate{}, errors.New("请选择模板所属客户端")
	}
	name := strings.TrimSpace(req.Name)
	prompt := strings.TrimSpace(req.Prompt)
	if name == "" || prompt == "" {
		return breakArmorSavedTemplate{}, errors.New("模板名称和内容不能为空")
	}
	if len([]byte(prompt)) > 128*1024 {
		return breakArmorSavedTemplate{}, errors.New("模板内容不能超过 128 KB")
	}
	id := strings.TrimSpace(req.ID)
	if id == "" || id == "v5" || id == "v35" {
		id = time.Now().Format("tpl-20060102-150405.000000000")
	}
	breakArmorTemplatesMu.Lock()
	defer breakArmorTemplatesMu.Unlock()
	store, err := loadBreakArmorTemplatesUnlocked(home)
	if err != nil {
		return breakArmorSavedTemplate{}, err
	}
	item := breakArmorSavedTemplate{ID: id, Client: client, Name: name, Prompt: prompt, UpdatedAt: time.Now()}
	replaced := false
	for i, current := range store.Templates {
		if current.ID == id && current.Client == client {
			store.Templates[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		store.Templates = append(store.Templates, item)
	}
	return item, saveBreakArmorTemplatesUnlocked(home, store)
}

func deleteBreakArmorTemplate(home string, req breakArmorTemplateRequest) error {
	id := strings.TrimSpace(req.ID)
	client := normalizeBreakArmorClient(req.Client)
	if id == "" || id == "v5" || id == "v35" {
		return errors.New("请选择可删除的自定义模板")
	}
	breakArmorTemplatesMu.Lock()
	defer breakArmorTemplatesMu.Unlock()
	store, err := loadBreakArmorTemplatesUnlocked(home)
	if err != nil {
		return err
	}
	out := store.Templates[:0]
	found := false
	for _, item := range store.Templates {
		if item.ID == id && (client == "" || item.Client == client) {
			found = true
			continue
		}
		out = append(out, item)
	}
	if !found {
		return errors.New("模板不存在")
	}
	store.Templates = out
	return saveBreakArmorTemplatesUnlocked(home, store)
}

func (a *app) handleBreakArmorTemplates(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := listBreakArmorTemplates(home, r.URL.Query().Get("client"))
		if err != nil {
			writeError(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]any{"templates": items})
	case http.MethodPost:
		var req breakArmorTemplateRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			writeError(w, 400, errors.New("模板请求格式无效"))
			return
		}
		if strings.EqualFold(req.Action, "delete") {
			if err := deleteBreakArmorTemplate(home, req); err != nil {
				writeError(w, 400, err)
				return
			}
			writeJSON(w, 200, map[string]any{"success": true})
			return
		}
		item, err := upsertBreakArmorTemplate(home, req)
		if err != nil {
			writeError(w, 400, err)
			return
		}
		writeJSON(w, 200, map[string]any{"success": true, "template": item})
	default:
		w.WriteHeader(405)
	}
}
