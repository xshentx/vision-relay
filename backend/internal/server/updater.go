package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Version is replaced by the platform build scripts for release builds.
var Version = "dev"

const (
	githubOwner   = "xshentx"
	githubRepo    = "vision-relay"
	maxUpdateSize = 256 << 20
)

var latestReleaseAPI = "https://api.github.com/repos/" + githubOwner + "/" + githubRepo + "/releases/latest"

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	HTMLURL     string        `json:"html_url"`
	Body        string        `json:"body"`
	PublishedAt time.Time     `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type updateInfo struct {
	CurrentVersion  string    `json:"current_version"`
	LatestVersion   string    `json:"latest_version,omitempty"`
	UpdateAvailable bool      `json:"update_available"`
	CanUpdate       bool      `json:"can_update"`
	ReleaseName     string    `json:"release_name,omitempty"`
	ReleaseURL      string    `json:"release_url,omitempty"`
	ReleaseNotes    string    `json:"release_notes,omitempty"`
	PublishedAt     time.Time `json:"published_at,omitempty"`
	AssetName       string    `json:"asset_name,omitempty"`
	AssetSize       int64     `json:"asset_size,omitempty"`
	release         githubRelease
	asset           githubAsset
}

type updateProgress struct {
	State           string    `json:"state"`
	Version         string    `json:"version,omitempty"`
	DownloadedBytes int64     `json:"downloaded_bytes"`
	TotalBytes      int64     `json:"total_bytes"`
	Percent         float64   `json:"percent"`
	Message         string    `json:"message,omitempty"`
	Error           string    `json:"error,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (a *app) handleUpdate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		info, err := a.checkForUpdate(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	case http.MethodPost:
		if !a.beginUpdate() {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":    map[string]string{"message": "更新任务正在进行中"},
				"progress": a.currentUpdateProgress(),
			})
			return
		}
		go a.runUpdate()
		writeJSON(w, http.StatusAccepted, map[string]any{
			"ok":       true,
			"message":  "更新任务已开始",
			"progress": a.currentUpdateProgress(),
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) handleUpdateProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, a.currentUpdateProgress())
}

func (a *app) beginUpdate() bool {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	switch a.updateStatus.State {
	case "checking", "downloading", "verifying", "installing", "restarting":
		return false
	}
	a.updateStatus = updateProgress{
		State:     "checking",
		Message:   "正在检查最新版本…",
		UpdatedAt: time.Now(),
	}
	return true
}

func (a *app) currentUpdateProgress() updateProgress {
	a.updateMu.RLock()
	progress := a.updateStatus
	a.updateMu.RUnlock()
	if progress.State == "" {
		progress.State = "idle"
		progress.Message = "尚未开始更新"
	}
	return progress
}

func (a *app) setUpdateProgress(progress updateProgress) {
	progress.UpdatedAt = time.Now()
	if progress.TotalBytes > 0 {
		progress.Percent = float64(progress.DownloadedBytes) / float64(progress.TotalBytes) * 100
		if progress.Percent > 100 {
			progress.Percent = 100
		}
	}
	a.updateMu.Lock()
	a.updateStatus = progress
	a.updateMu.Unlock()
}

func (a *app) failUpdate(err error) {
	progress := a.currentUpdateProgress()
	progress.State = "error"
	progress.Message = "更新失败"
	progress.Error = err.Error()
	a.setUpdateProgress(progress)
}

func (a *app) runUpdate() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	info, err := a.checkForUpdate(ctx)
	if err != nil {
		a.failUpdate(err)
		return
	}
	if !info.UpdateAvailable {
		a.failUpdate(errors.New("当前已是最新版本"))
		return
	}
	if !info.CanUpdate {
		a.failUpdate(errors.New("当前运行方式不支持自动替换，请下载 Release 后手动更新"))
		return
	}

	report := func(state string, downloaded, total int64) {
		message := "正在下载更新…"
		if state == "verifying" {
			message = "下载完成，正在校验更新文件…"
		}
		a.setUpdateProgress(updateProgress{
			State:           state,
			Version:         info.LatestVersion,
			DownloadedBytes: downloaded,
			TotalBytes:      total,
			Message:         message,
		})
	}
	report("downloading", 0, info.AssetSize)
	downloaded, err := a.downloadUpdate(ctx, info, report)
	if err != nil {
		a.failUpdate(err)
		return
	}

	completed := a.currentUpdateProgress()
	a.setUpdateProgress(updateProgress{
		State:           "installing",
		Version:         info.LatestVersion,
		DownloadedBytes: completed.DownloadedBytes,
		TotalBytes:      completed.TotalBytes,
		Message:         "校验通过，正在准备安装…",
	})
	target, err := os.Executable()
	if err != nil {
		_ = os.Remove(downloaded)
		a.failUpdate(err)
		return
	}
	if err := startUpdateHelper(downloaded, target, os.Getpid(), os.Args[1:]); err != nil {
		_ = os.Remove(downloaded)
		a.failUpdate(fmt.Errorf("启动更新程序失败: %w", err))
		return
	}
	a.setUpdateProgress(updateProgress{
		State:           "restarting",
		Version:         info.LatestVersion,
		DownloadedBytes: completed.DownloadedBytes,
		TotalBytes:      completed.TotalBytes,
		Message:         "更新已下载，程序即将重启…",
	})
	time.Sleep(800 * time.Millisecond)
	os.Exit(0)
}

func (a *app) checkForUpdate(ctx context.Context) (updateInfo, error) {
	release, err := a.fetchLatestRelease(ctx)
	if err != nil {
		return updateInfo{}, err
	}
	asset, ok := selectReleaseAsset(release.Assets, runtime.GOOS, runtime.GOARCH)
	info := updateInfo{
		CurrentVersion: Version, LatestVersion: release.TagName,
		UpdateAvailable: versionNewer(release.TagName, Version),
		CanUpdate:       runtime.GOOS == "windows" && Version != "dev" && ok && strings.EqualFold(filepath.Ext(executablePath()), ".exe"),
		ReleaseName:     release.Name, ReleaseURL: release.HTMLURL, ReleaseNotes: release.Body,
		PublishedAt: release.PublishedAt, release: release,
	}
	if ok {
		info.AssetName, info.AssetSize, info.asset = asset.Name, asset.Size, asset
	}
	return info, nil
}

func (a *app) fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPI, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "vision-relay/"+Version)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return githubRelease{}, fmt.Errorf("检查 GitHub 更新失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return githubRelease{}, fmt.Errorf("GitHub 返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&release); err != nil {
		return githubRelease{}, err
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubRelease{}, errors.New("GitHub Release 缺少版本标签")
	}
	return release, nil
}

func (a *app) downloadUpdate(ctx context.Context, info updateInfo, report func(state string, downloaded, total int64)) (string, error) {
	if info.asset.BrowserDownloadURL == "" {
		return "", errors.New("Release 中没有 Windows 可执行程序")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "vision-relay/"+Version)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载更新失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载更新失败: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxUpdateSize {
		return "", errors.New("更新文件超过 256 MB 限制")
	}
	total := resp.ContentLength
	if total <= 0 {
		total = info.AssetSize
	}
	if report != nil {
		report("downloading", 0, total)
	}
	file, err := os.CreateTemp("", "vision-relay-update-*.exe")
	if err != nil {
		return "", err
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	hash := sha256.New()
	progressWriter := &updateProgressWriter{total: total, report: report}
	n, err := io.Copy(io.MultiWriter(file, hash, progressWriter), io.LimitReader(resp.Body, maxUpdateSize+1))
	if err != nil {
		return "", err
	}
	if n > maxUpdateSize {
		return "", errors.New("更新文件超过 256 MB 限制")
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	header := make([]byte, 2)
	check, err := os.Open(path)
	if err != nil {
		return "", err
	}
	_, readErr := io.ReadFull(check, header)
	_ = check.Close()
	if readErr != nil || string(header) != "MZ" {
		return "", errors.New("下载内容不是有效的 Windows 可执行程序")
	}
	if report != nil {
		report("verifying", n, total)
	}
	if expected, found, err := a.fetchChecksum(ctx, info.release.Assets, info.asset.Name); err != nil {
		return "", err
	} else if found && !strings.EqualFold(expected, hex.EncodeToString(hash.Sum(nil))) {
		return "", errors.New("更新文件 SHA-256 校验失败")
	}
	ok = true
	return path, nil
}

type updateProgressWriter struct {
	downloaded int64
	total      int64
	report     func(state string, downloaded, total int64)
	lastReport time.Time
}

func (w *updateProgressWriter) Write(p []byte) (int, error) {
	w.downloaded += int64(len(p))
	now := time.Now()
	finished := w.total > 0 && w.downloaded >= w.total
	if w.report != nil && (w.lastReport.IsZero() || now.Sub(w.lastReport) >= 100*time.Millisecond || finished) {
		w.report("downloading", w.downloaded, w.total)
		w.lastReport = now
	}
	return len(p), nil
}

func (a *app) fetchChecksum(ctx context.Context, assets []githubAsset, exeName string) (string, bool, error) {
	wanted := strings.ToLower(exeName + ".sha256")
	for _, asset := range assets {
		if strings.ToLower(asset.Name) != wanted {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
		if err != nil {
			return "", true, err
		}
		req.Header.Set("User-Agent", "vision-relay/"+Version)
		resp, err := a.httpClient.Do(req)
		if err != nil {
			return "", true, err
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if readErr != nil {
			return "", true, readErr
		}
		if resp.StatusCode != http.StatusOK {
			return "", true, fmt.Errorf("下载校验文件失败: HTTP %d", resp.StatusCode)
		}
		fields := strings.Fields(string(data))
		if len(fields) == 0 || len(fields[0]) != 64 {
			return "", true, errors.New("SHA-256 校验文件格式无效")
		}
		return fields[0], true, nil
	}
	return "", false, nil
}

func selectReleaseAsset(assets []githubAsset, goos, goarch string) (githubAsset, bool) {
	switch goos {
	case "windows":
		return selectWindowsAssetForArch(assets, goarch)
	case "darwin":
		return selectDarwinAsset(assets, goarch)
	default:
		return githubAsset{}, false
	}
}

func selectWindowsAsset(assets []githubAsset) (githubAsset, bool) {
	return selectWindowsAssetForArch(assets, runtime.GOARCH)
}

func selectWindowsAssetForArch(assets []githubAsset, goarch string) (githubAsset, bool) {
	for _, asset := range assets {
		if strings.EqualFold(asset.Name, "vision-relay.exe") {
			return asset, true
		}
	}
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.HasSuffix(name, ".exe") && strings.Contains(name, "windows") && releaseAssetMatchesArch(name, goarch) {
			return asset, true
		}
	}
	return githubAsset{}, false
}

func selectDarwinAsset(assets []githubAsset, goarch string) (githubAsset, bool) {
	canonical := []string{
		"vision-relay-darwin-" + goarch + ".zip",
		"vision-relay-macos-" + goarch + ".zip",
		"vision-relay-darwin-universal.zip",
		"vision-relay-macos-universal.zip",
	}
	for _, wanted := range canonical {
		for _, asset := range assets {
			if strings.EqualFold(asset.Name, wanted) {
				return asset, true
			}
		}
	}
	for _, asset := range assets {
		name := strings.ToLower(strings.ReplaceAll(asset.Name, "_", "-"))
		if (strings.Contains(name, "darwin") || strings.Contains(name, "macos") || strings.Contains(name, "mac-os")) &&
			(strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar.gz")) && releaseAssetMatchesArch(name, goarch) {
			return asset, true
		}
	}
	return githubAsset{}, false
}

func releaseAssetMatchesArch(name, goarch string) bool {
	name = strings.ToLower(strings.ReplaceAll(name, "_", "-"))
	if strings.Contains(name, "universal") {
		return true
	}
	switch goarch {
	case "amd64":
		return strings.Contains(name, "amd64") || strings.Contains(name, "x86-64") || strings.Contains(name, "x64")
	case "arm64":
		return strings.Contains(name, "arm64") || strings.Contains(name, "aarch64")
	default:
		return strings.Contains(name, strings.ToLower(goarch))
	}
}

func versionNewer(latest, current string) bool {
	if current == "" || current == "dev" {
		return true
	}
	l, lok := numericVersion(latest)
	c, cok := numericVersion(current)
	if !lok || !cok {
		return strings.TrimPrefix(latest, "v") != strings.TrimPrefix(current, "v")
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func numericVersion(value string) ([3]int, bool) {
	var out [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "-", 2)[0]
	parts := strings.Split(value, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func executablePath() string { p, _ := os.Executable(); return p }
