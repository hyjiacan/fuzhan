package githubrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const GitHubReleaseTempFileSuffix = ".uploading"

type GitHubReleaseDownloader struct {
	client     *http.Client
	dispatcher *GitHubReleaseEventDispatcher
}

func NewGitHubReleaseDownloader(client *http.Client, dispatcher *GitHubReleaseEventDispatcher) *GitHubReleaseDownloader {
	return &GitHubReleaseDownloader{client: client, dispatcher: dispatcher}
}

// DownloadAsset 执行单个资产的下载，包含完整的容错与续传逻辑
func (d *GitHubReleaseDownloader) DownloadAsset(ctx context.Context, exec *GitHubReleaseExecution, asset *GitHubReleaseAssetDownload, accelerator string) error {
	d.dispatcher.DispatchEvent(GitHubReleaseEvent{
		Type: GitHubReleaseEventAssetDownloadStart, ExecutionID: exec.ID, TaskID: exec.TaskID, AssetID: asset.ID,
	})

	// 1. 确定本地路径与临时文件路径 (.uploading)
	finalPath := asset.LocalPath
	if finalPath == "" {
		finalPath = filepath.Join(exec.ConfigSnapshot.CustomOutputDir, asset.AssetName)
	}
	tempPath := finalPath + GitHubReleaseTempFileSuffix

	// 2. 检查现场，准备断点续传
	var downloadedBytes int64 = 0
	if fileInfo, err := os.Stat(tempPath); err == nil {
		downloadedBytes = fileInfo.Size()
	}

	// 3. 构建请求 (优先加速，准备降级)
	targetURL := asset.OriginalURL
	if accelerator != "" {
		targetURL = accelerator + "/" + asset.OriginalURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return d.handleFailure(ctx, exec, asset, tempPath, fmt.Errorf("build req error: %w", err))
	}

	if asset.SupportsRange && downloadedBytes > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", downloadedBytes))
	}

	resp, err := d.client.Do(req)

	// 4. 自动降级 (Fallback) 逻辑
	if err != nil || (resp != nil && resp.StatusCode >= 400) {
		if accelerator != "" {
			req, _ = http.NewRequestWithContext(ctx, http.MethodGet, asset.OriginalURL, nil)
			if asset.SupportsRange && downloadedBytes > 0 {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-", downloadedBytes))
			}
			resp, err = d.client.Do(req)
		}
	}

	if err != nil {
		return d.handleFailure(ctx, exec, asset, tempPath, fmt.Errorf("network error: %w", err))
	}
	defer resp.Body.Close()

	if downloadedBytes == 0 {
		asset.SupportsRange = resp.Header.Get("Accept-Ranges") == "bytes"
	}

	// 5. 写入 .uploading 文件
	outFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return d.handleFailure(ctx, exec, asset, tempPath, fmt.Errorf("open file error: %w", err))
	}
	defer outFile.Close()

	var hasher hash.Hash
	if asset.ChecksumAlgo == "sha256" {
		hasher = sha256.New()
	}

	written, err := io.Copy(outFile, io.TeeReader(resp.Body, hasher))
	if err != nil {
		return d.handleFailure(ctx, exec, asset, tempPath, fmt.Errorf("io copy error: %w", err))
	}
	asset.BytesDownloaded += written

	// 6. 校验 Checksum
	if hasher != nil && asset.ChecksumExpect != "" {
		actualChecksum := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actualChecksum, asset.ChecksumExpect) {
			// 校验失败契约：必须物理删除损坏的 .uploading 文件，重置进度
			os.Remove(tempPath)
			asset.BytesDownloaded = 0
			asset.Status = GitHubReleaseAssetChecksumFail
			return d.handleFailure(ctx, exec, asset, "", fmt.Errorf("checksum mismatch: expected %s, got %s", asset.ChecksumExpect, actualChecksum))
		}
	}

	// 7. 下载完成，原子重命名
	if err := os.Rename(tempPath, finalPath); err != nil {
		return d.handleFailure(ctx, exec, asset, tempPath, fmt.Errorf("rename error: %w", err))
	}

	asset.Status = GitHubReleaseAssetCompleted
	asset.LocalPath = finalPath
	d.dispatcher.DispatchEvent(GitHubReleaseEvent{
		Type: GitHubReleaseEventAssetDownloadDone, ExecutionID: exec.ID, TaskID: exec.TaskID, AssetID: asset.ID,
		Payload: map[string]any{"bytes": asset.BytesDownloaded, "path": finalPath},
	})
	return nil
}

func (d *GitHubReleaseDownloader) handleFailure(ctx context.Context, exec *GitHubReleaseExecution, asset *GitHubReleaseAssetDownload, tempPath string, err error) error {
	asset.Status = GitHubReleaseAssetFailed
	asset.RetryCount++
	asset.LastError = err.Error()

	if asset.RetryCount >= asset.MaxRetries {
		asset.Status = GitHubReleaseAssetDeadLetter
	}

	d.dispatcher.DispatchEvent(GitHubReleaseEvent{
		Type: GitHubReleaseEventAssetDownloadFail, ExecutionID: exec.ID, TaskID: exec.TaskID, AssetID: asset.ID,
		Payload: map[string]any{"error": err.Error(), "retry_count": asset.RetryCount},
	})
	return err
}
