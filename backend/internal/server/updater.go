package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
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

var (
	latestReleaseAPI  = "https://api.github.com/repos/" + githubOwner + "/" + githubRepo + "/releases/latest"
	latestReleaseFeed = "https://github.com/" + githubOwner + "/" + githubRepo + "/releases.atom"
)

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

type githubReleaseFeed struct {
	Entries []githubReleaseFeedEntry `xml:"entry"`
}

type githubReleaseFeedEntry struct {
	Title   string                  `xml:"title"`
	Updated time.Time               `xml:"updated"`
	Content string                  `xml:"content"`
	Links   []githubReleaseFeedLink `xml:"link"`
}

type githubReleaseFeedLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
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
	target, err := os.Executable()
	if err != nil {
		a.failUpdate(fmt.Errorf("定位当前程序失败: %w", err))
		return
	}
	report("downloading", 0, info.AssetSize)
	downloaded, err := a.downloadUpdate(ctx, info, filepath.Dir(target), report)
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
	if err := installUpdate(downloaded, target, os.Args[1:]); err != nil {
		_ = os.Remove(downloaded)
		a.failUpdate(fmt.Errorf("安装并重启更新失败: %w", err))
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
	if ok && asset.Size <= 0 {
		asset.Size = a.fetchReleaseAssetSize(ctx, asset.BrowserDownloadURL)
	}
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
	// Anonymous GitHub REST requests share a small per-IP quota. The Atom feed
	// is public, cacheable, and does not consume that quota, so use it by
	// default. Users that explicitly provide a token still get the richer REST
	// metadata, with the feed retained as a rate-limit/outage fallback.
	token := strings.TrimSpace(os.Getenv("VISION_RELAY_GITHUB_TOKEN"))
	if token == "" {
		release, feedErr := a.fetchLatestReleaseFeed(ctx)
		if feedErr == nil {
			return release, nil
		}
		release, apiErr := a.fetchLatestReleaseAPI(ctx, "")
		if apiErr == nil {
			return release, nil
		}
		return githubRelease{}, fmt.Errorf("检查 GitHub 更新失败（Release feed: %v；REST API: %v）", feedErr, apiErr)
	}

	release, apiErr := a.fetchLatestReleaseAPI(ctx, token)
	if apiErr == nil {
		return release, nil
	}
	release, feedErr := a.fetchLatestReleaseFeed(ctx)
	if feedErr == nil {
		return release, nil
	}
	return githubRelease{}, fmt.Errorf("检查 GitHub 更新失败（REST API: %v；Release feed: %v）", apiErr, feedErr)
}

func (a *app) fetchLatestReleaseAPI(ctx context.Context, token string) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPI, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "vision-relay/"+Version)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
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

func (a *app) fetchLatestReleaseFeed(ctx context.Context) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseFeed, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/atom+xml")
	req.Header.Set("User-Agent", "vision-relay/"+Version)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return githubRelease{}, fmt.Errorf("读取 GitHub Release feed 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return githubRelease{}, fmt.Errorf("GitHub Release feed 返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var feed githubReleaseFeed
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&feed); err != nil {
		return githubRelease{}, fmt.Errorf("解析 GitHub Release feed 失败: %w", err)
	}
	if len(feed.Entries) == 0 {
		return githubRelease{}, errors.New("GitHub Release feed 中没有发行版")
	}
	entry := feed.Entries[0]
	releaseURL := ""
	for _, link := range entry.Links {
		if link.Rel == "alternate" && strings.TrimSpace(link.Href) != "" {
			releaseURL = strings.TrimSpace(link.Href)
			break
		}
	}
	tag, err := releaseTagFromURL(releaseURL)
	if err != nil {
		return githubRelease{}, err
	}
	name := strings.TrimSpace(entry.Title)
	if name == "" {
		name = tag
	}
	return githubRelease{
		TagName:     tag,
		Name:        name,
		HTMLURL:     releaseURL,
		Body:        releaseNotesFromHTML(entry.Content),
		PublishedAt: entry.Updated,
		Assets:      canonicalReleaseAssets(tag),
	}, nil
}

func releaseTagFromURL(releaseURL string) (string, error) {
	prefix := "https://github.com/" + githubOwner + "/" + githubRepo + "/releases/tag/"
	if !strings.HasPrefix(releaseURL, prefix) {
		return "", errors.New("GitHub Release feed 缺少有效的发行版链接")
	}
	tag, err := url.PathUnescape(strings.TrimPrefix(releaseURL, prefix))
	if err != nil || strings.TrimSpace(tag) == "" || strings.Contains(tag, "/") {
		return "", errors.New("GitHub Release feed 缺少有效的版本标签")
	}
	return tag, nil
}

func canonicalReleaseAssets(tag string) []githubAsset {
	base := "https://github.com/" + githubOwner + "/" + githubRepo + "/releases/download/" + url.PathEscape(tag) + "/"
	names := []string{
		"vision-relay.exe",
		"vision-relay.exe.sha256",
		"vision-relay-darwin-universal.zip",
		"vision-relay-darwin-universal.zip.sha256",
	}
	assets := make([]githubAsset, 0, len(names))
	for _, name := range names {
		assets = append(assets, githubAsset{Name: name, BrowserDownloadURL: base + url.PathEscape(name)})
	}
	return assets
}

func releaseNotesFromHTML(value string) string {
	value = strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</p>", "\n", "</h1>", "\n", "</h2>", "\n", "</h3>", "\n",
		"<li>", "- ", "</li>", "\n", "</ul>", "\n", "</ol>", "\n",
		"</blockquote>", "\n",
	).Replace(value)
	var plain strings.Builder
	plain.Grow(len(value))
	inTag := false
	for _, r := range value {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				plain.WriteRune(r)
			}
		}
	}
	lines := strings.Split(html.UnescapeString(plain.String()), "\n")
	cleaned := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(cleaned) > 0 && !blank {
				cleaned = append(cleaned, "")
			}
			blank = true
			continue
		}
		cleaned = append(cleaned, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func (a *app) fetchReleaseAssetSize(ctx context.Context, downloadURL string) int64 {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, downloadURL, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", "vision-relay/"+Version)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return 0
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength <= 0 || resp.ContentLength > maxUpdateSize {
		return 0
	}
	return resp.ContentLength
}

func (a *app) downloadUpdate(ctx context.Context, info updateInfo, destinationDir string, report func(state string, downloaded, total int64)) (string, error) {
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
	// Keep one predictably named, non-executable staging file beside the running
	// application. The staging path is never passed to CreateProcess.
	path := filepath.Join(destinationDir, "vision-relay.update")
	_ = os.Remove(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("在程序目录创建更新文件失败: %w", err)
	}
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
	} else if !found {
		return "", errors.New("Release 缺少更新文件的 SHA-256 校验文件")
	} else if !strings.EqualFold(expected, hex.EncodeToString(hash.Sum(nil))) {
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
		if _, err := hex.DecodeString(fields[0]); err != nil {
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
