package routing

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/utils"
)

var inboundMediaHTTPClient = &http.Client{Timeout: 60 * time.Second}

const inboundMediaMaxDownloadBytes int64 = 64 * 1024 * 1024

func prepareInboundMedia(ctx context.Context, workspace string, msg *bus.InboundMessage) error {
	if msg == nil || strings.TrimSpace(workspace) == "" || len(msg.Media) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	stageDir := filepath.Join(
		workspace,
		".sciclaw",
		"inbound",
		sanitizePathSegment(msg.Channel),
		inboundMediaMessageDir(msg),
	)
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return fmt.Errorf("create inbound media staging dir: %w", err)
	}

	seenNames := map[string]int{}
	stagedPaths := make([]string, 0, len(msg.Media))
	stagedLines := make([]string, 0, len(msg.Media))
	failedLines := make([]string, 0)
	var firstErr error

	for i, source := range msg.Media {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}

		filename := uniqueInboundFilename(deriveInboundFilename(source, i+1), seenNames)
		destPath := filepath.Join(stageDir, filename)
		var err error
		switch {
		case isHTTPURL(source):
			err = downloadInboundURL(ctx, source, destPath)
		default:
			err = copyInboundFile(source, destPath)
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			logger.WarnCF("routing", "Failed to stage inbound attachment", map[string]any{
				"channel":   msg.Channel,
				"chat_id":   msg.ChatID,
				"source":    source,
				"dest_path": destPath,
				"error":     err.Error(),
			})
			failedLines = append(failedLines, fmt.Sprintf("- %s", filename))
			continue
		}

		relPath, err := filepath.Rel(workspace, destPath)
		if err != nil {
			relPath = destPath
		}
		relPath = filepath.ToSlash(relPath)

		stagedPaths = append(stagedPaths, destPath)
		stagedLines = append(stagedLines, fmt.Sprintf("- %s -> %s", filename, relPath))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(stagedLines) == 0 && len(failedLines) > 0 && firstErr != nil {
		return fmt.Errorf("failed to stage inbound attachments: %w", firstErr)
	}

	if len(stagedLines) > 0 {
		msg.Content = appendInboundContent(msg.Content, "Attachments staged locally and available to tools:\n"+strings.Join(stagedLines, "\n"))
		msg.Media = stagedPaths
	}
	if len(failedLines) > 0 {
		msg.Content = appendInboundContent(msg.Content, "Attachments that could not be staged locally:\n"+strings.Join(failedLines, "\n"))
	}

	return nil
}

func inboundMediaMessageDir(msg *bus.InboundMessage) string {
	if msg == nil {
		return "unknown-message"
	}
	if id := strings.TrimSpace(msg.Metadata["message_id"]); id != "" {
		return sanitizePathSegment(id)
	}
	if key := strings.TrimSpace(msg.SessionKey); key != "" {
		return sanitizePathSegment(key)
	}
	return "unknown-message"
}

func sanitizePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		return "unknown"
	}
	return out
}

func deriveInboundFilename(source string, fallbackIndex int) string {
	name := ""
	if u, err := url.Parse(source); err == nil && u.Scheme != "" && u.Host != "" {
		name = path.Base(u.Path)
	} else {
		name = filepath.Base(source)
	}
	name = utils.SanitizeFilename(name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		return fmt.Sprintf("attachment-%d.bin", fallbackIndex)
	}
	return name
}

func uniqueInboundFilename(name string, seen map[string]int) string {
	count := seen[name]
	seen[name] = count + 1
	if count == 0 {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s-%d%s", base, count+1, ext)
}

func isHTTPURL(value string) bool {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	default:
		return false
	}
}

func downloadInboundURL(ctx context.Context, sourceURL, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	resp, err := inboundMediaHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if resp.ContentLength > inboundMediaMaxDownloadBytes {
		return fmt.Errorf("attachment exceeds %d bytes", inboundMediaMaxDownloadBytes)
	}
	return writeInboundFile(io.LimitReader(resp.Body, inboundMediaMaxDownloadBytes+1), destPath, inboundMediaMaxDownloadBytes)
}

func copyInboundFile(sourcePath, destPath string) error {
	in, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeInboundFile(in, destPath, 0)
}

func writeInboundFile(r io.Reader, destPath string, maxBytes int64) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	written, err := io.Copy(out, r)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(destPath)
		return err
	}
	if maxBytes > 0 && written > maxBytes {
		_ = out.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("attachment exceeds %d bytes", maxBytes)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(destPath)
		return err
	}
	return nil
}

func appendInboundContent(content, suffix string) string {
	content = strings.TrimSpace(content)
	suffix = strings.TrimSpace(suffix)
	switch {
	case content == "":
		return suffix
	case suffix == "":
		return content
	default:
		return content + "\n" + suffix
	}
}
