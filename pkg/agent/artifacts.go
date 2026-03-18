package agent

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/session"
)

func (al *AgentLoop) registerInboundArtifacts(sessionKey string, media []string) {
	if strings.TrimSpace(sessionKey) == "" || len(media) == 0 {
		return
	}
	artifacts := make([]session.Artifact, 0, len(media))
	for _, item := range media {
		path := canonicalArtifactPath(al.workspace, item)
		if path == "" {
			continue
		}
		artifacts = append(artifacts, session.Artifact{
			Role:   "input",
			Path:   path,
			Label:  filepath.Base(path),
			Source: "inbound_media",
		})
	}
	al.sessions.RegisterArtifacts(sessionKey, artifacts...)
}

func (al *AgentLoop) registerArtifactsFromToolCall(sessionKey, toolName string, args map[string]interface{}) {
	if strings.TrimSpace(sessionKey) == "" {
		return
	}
	artifacts := artifactsFromToolCall(al.workspace, toolName, args)
	if len(artifacts) == 0 {
		return
	}
	al.sessions.RegisterArtifacts(sessionKey, artifacts...)
}

func artifactsFromToolCall(workspace, toolName string, args map[string]interface{}) []session.Artifact {
	add := func(dst []session.Artifact, role, source, rawPath string) []session.Artifact {
		path := canonicalArtifactPath(workspace, rawPath)
		if path == "" {
			return dst
		}
		return append(dst, session.Artifact{
			Role:   role,
			Path:   path,
			Label:  filepath.Base(path),
			Source: source,
		})
	}

	artifacts := []session.Artifact{}
	switch toolName {
	case "write_file", "edit_file", "append_file":
		artifacts = add(artifacts, "output", toolName, stringArg(args, "path"))
	case "docx_review_read", "xlsx_review_read", "pptx_review_read":
		artifacts = add(artifacts, "input", toolName, stringArg(args, "input_path"))
	case "docx_review_apply", "xlsx_review_apply", "pptx_review_apply":
		artifacts = add(artifacts, "input", toolName, stringArg(args, "input_path"))
		artifacts = add(artifacts, "output", toolName, stringArg(args, "output_path"))
	case "pdf_form_inspect", "pdf_form_schema":
		artifacts = add(artifacts, "input", toolName, stringArg(args, "pdf_path"))
	case "pdf_form_fill":
		artifacts = add(artifacts, "input", toolName, stringArg(args, "pdf_path"))
		artifacts = add(artifacts, "input", toolName, stringArg(args, "values_path"))
		artifacts = add(artifacts, "output", toolName, stringArg(args, "output_path"))
	case "pubmed_export_ris":
		artifacts = add(artifacts, "output", toolName, stringArg(args, "output_file"))
	case "message":
		for _, path := range attachmentPaths(args["attachments"]) {
			artifacts = add(artifacts, "output", toolName, path)
		}
	}
	return artifacts
}

func canonicalArtifactPath(workspace, rawPath string) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return ""
	}
	if parsed, err := url.Parse(rawPath); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return ""
	}
	if !filepath.IsAbs(rawPath) {
		if strings.TrimSpace(workspace) == "" {
			rawPath = filepath.Clean(rawPath)
		} else {
			rawPath = filepath.Join(workspace, rawPath)
		}
	}
	return filepath.Clean(rawPath)
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key]
	if !ok {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(str)
}

func attachmentPaths(raw interface{}) []string {
	items, ok := raw.([]interface{})
	if !ok || len(items) == 0 {
		return nil
	}

	paths := make([]string, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		path, ok := entry["path"].(string)
		if !ok || strings.TrimSpace(path) == "" {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}
