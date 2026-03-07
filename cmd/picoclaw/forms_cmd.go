package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type formsInspectOptions struct {
	PDFPath  string
	JSONOut  string
	ShowHelp bool
}

type formsFillOptions struct {
	PDFPath      string
	OutPath      string
	SourcePath   string
	ValuesJSON   string
	Model        string
	OllamaURL    string
	JSONOut      string
	RawOut       string
	NoSynthetic  bool
	SkipBackfill bool
	ShowHelp     bool
}

var formsPythonPackages = []string{"requests", "pypdf"}

func formsCmd() {
	args := os.Args[2:]
	if len(args) == 0 {
		formsHelp()
		return
	}

	switch args[0] {
	case "inspect":
		formsInspectCmd(args[1:])
	case "fill":
		formsFillCmd(args[1:])
	case "help", "-h", "--help":
		formsHelp()
	default:
		fmt.Printf("Unknown forms command: %s\n", args[0])
		formsHelp()
	}
}

func formsHelp() {
	commandName := invokedCLIName()
	fmt.Println("\nForms commands:")
	fmt.Println("  inspect                 Verify PDF is a true AcroForm and print field summary")
	fmt.Println("  fill                    Fill an AcroForm from source text (LLM) or values JSON")
	fmt.Println()
	fmt.Println("Inspect options:")
	fmt.Println("  --pdf <path>            Input PDF path (required)")
	fmt.Println("  --json-out <path>       Optional JSON output path")
	fmt.Println()
	fmt.Println("Fill options:")
	fmt.Println("  --pdf <path>            Input AcroForm PDF path (required)")
	fmt.Println("  --out <path>            Output filled PDF path (required)")
	fmt.Println("  --source <path>         Source text file for LLM extraction")
	fmt.Println("  --values <path>         JSON map for direct fill (alternative to --source)")
	fmt.Println("  --model <name>          Ollama model (default: qwen3.5:9b)")
	fmt.Println("  --ollama-url <url>      Ollama base URL (default: http://localhost:11434)")
	fmt.Println("  --json-out <path>       Optional fill summary JSON path")
	fmt.Println("  --raw-out <path>        Optional raw LLM response path")
	fmt.Println("  --no-synthetic          Do not synthesize placeholder identity fields")
	fmt.Println("  --skip-backfill         Skip second pass for missing text fields")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  %s forms inspect --pdf ~/Downloads/form.pdf\n", commandName)
	fmt.Printf("  %s forms fill --pdf form.pdf --source input.txt --out form_filled.pdf\n", commandName)
	fmt.Printf("  %s forms fill --pdf form.pdf --values values.json --out form_filled.pdf\n", commandName)
}

func parseFormsInspectOptions(args []string) (formsInspectOptions, error) {
	opts := formsInspectOptions{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--pdf":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--pdf requires a value")
			}
			opts.PDFPath = args[i+1]
			i++
		case "--json-out":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--json-out requires a value")
			}
			opts.JSONOut = args[i+1]
			i++
		case "help", "-h", "--help":
			opts.ShowHelp = true
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if opts.ShowHelp {
		return opts, nil
	}
	if strings.TrimSpace(opts.PDFPath) == "" {
		return opts, fmt.Errorf("--pdf is required")
	}
	return opts, nil
}

func parseFormsFillOptions(args []string) (formsFillOptions, error) {
	opts := formsFillOptions{
		Model:     "qwen3.5:9b",
		OllamaURL: "http://localhost:11434",
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--pdf":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--pdf requires a value")
			}
			opts.PDFPath = args[i+1]
			i++
		case "--out":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--out requires a value")
			}
			opts.OutPath = args[i+1]
			i++
		case "--source":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--source requires a value")
			}
			opts.SourcePath = args[i+1]
			i++
		case "--values":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--values requires a value")
			}
			opts.ValuesJSON = args[i+1]
			i++
		case "--model":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--model requires a value")
			}
			opts.Model = args[i+1]
			i++
		case "--ollama-url":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--ollama-url requires a value")
			}
			opts.OllamaURL = args[i+1]
			i++
		case "--json-out":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--json-out requires a value")
			}
			opts.JSONOut = args[i+1]
			i++
		case "--raw-out":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--raw-out requires a value")
			}
			opts.RawOut = args[i+1]
			i++
		case "--no-synthetic":
			opts.NoSynthetic = true
		case "--skip-backfill":
			opts.SkipBackfill = true
		case "help", "-h", "--help":
			opts.ShowHelp = true
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if opts.ShowHelp {
		return opts, nil
	}
	if strings.TrimSpace(opts.PDFPath) == "" {
		return opts, fmt.Errorf("--pdf is required")
	}
	if strings.TrimSpace(opts.OutPath) == "" {
		return opts, fmt.Errorf("--out is required")
	}
	hasSource := strings.TrimSpace(opts.SourcePath) != ""
	hasValues := strings.TrimSpace(opts.ValuesJSON) != ""
	if !hasSource && !hasValues {
		return opts, fmt.Errorf("provide one of --source or --values")
	}
	if hasSource && hasValues {
		return opts, fmt.Errorf("use either --source or --values, not both")
	}
	return opts, nil
}

func formsInspectCmd(args []string) {
	opts, err := parseFormsInspectOptions(args)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		formsHelp()
		os.Exit(2)
	}
	if opts.ShowHelp {
		formsHelp()
		return
	}

	pythonPath, err := ensureFormsPythonRuntime()
	if err != nil {
		fmt.Printf("Error preparing Python runtime: %v\n", err)
		os.Exit(1)
	}

	payload := map[string]string{
		"pdf": cleanPath(opts.PDFPath),
	}
	out, err := runFormsPython(pythonPath, "inspect", payload)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if opts.JSONOut != "" {
		if err := os.WriteFile(cleanPath(opts.JSONOut), out, 0644); err != nil {
			fmt.Printf("Error writing JSON output: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Wrote JSON summary: %s\n", cleanPath(opts.JSONOut))
	}

	var summary map[string]interface{}
	if err := json.Unmarshal(out, &summary); err != nil {
		fmt.Printf("AcroForm verification passed.\n")
		fmt.Println(string(out))
		return
	}

	if ok, _ := summary["ok"].(bool); !ok {
		fmt.Printf("Not a valid AcroForm PDF: %v\n", summary["error"])
		os.Exit(1)
	}

	fmt.Println("✓ AcroForm verification passed")
	fmt.Printf("  PDF: %s\n", cleanPath(opts.PDFPath))
	fmt.Printf("  Fields: %v\n", summary["field_count"])
	fmt.Printf("  Widgets: %v\n", summary["widget_count"])
	fmt.Printf("  Pages: %v\n", summary["page_count"])
}

func formsFillCmd(args []string) {
	opts, err := parseFormsFillOptions(args)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		formsHelp()
		os.Exit(2)
	}
	if opts.ShowHelp {
		formsHelp()
		return
	}

	pythonPath, err := ensureFormsPythonRuntime()
	if err != nil {
		fmt.Printf("Error preparing Python runtime: %v\n", err)
		os.Exit(1)
	}

	payload := map[string]string{
		"pdf":           cleanPath(opts.PDFPath),
		"out":           cleanPath(opts.OutPath),
		"model":         strings.TrimSpace(opts.Model),
		"ollama_url":    strings.TrimSpace(opts.OllamaURL),
		"no_synthetic":  fmt.Sprintf("%t", opts.NoSynthetic),
		"skip_backfill": fmt.Sprintf("%t", opts.SkipBackfill),
	}
	if opts.SourcePath != "" {
		payload["source"] = cleanPath(opts.SourcePath)
	}
	if opts.ValuesJSON != "" {
		payload["values"] = cleanPath(opts.ValuesJSON)
	}
	if opts.JSONOut != "" {
		payload["json_out"] = cleanPath(opts.JSONOut)
	}
	if opts.RawOut != "" {
		payload["raw_out"] = cleanPath(opts.RawOut)
	}

	out, err := runFormsPython(pythonPath, "fill", payload)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	var summary map[string]interface{}
	if err := json.Unmarshal(out, &summary); err != nil {
		fmt.Println("Fill completed.")
		fmt.Println(string(out))
		return
	}

	if ok, _ := summary["ok"].(bool); !ok {
		fmt.Printf("Fill failed: %v\n", summary["error"])
		os.Exit(1)
	}

	fmt.Println("✓ Fill completed")
	fmt.Printf("  Input: %s\n", cleanPath(opts.PDFPath))
	fmt.Printf("  Output: %s\n", cleanPath(opts.OutPath))
	fmt.Printf("  AcroForm fields: %v\n", summary["field_count"])
	fmt.Printf("  Widgets: %v\n", summary["widget_count"])
	fmt.Printf("  Text fields non-empty: %v\n", summary["text_fields_nonempty"])
	fmt.Printf("  Checkbox yes count: %v\n", summary["checkbox_checked_yes"])
	if opts.JSONOut != "" {
		fmt.Printf("  JSON summary: %s\n", cleanPath(opts.JSONOut))
	}
	if opts.RawOut != "" {
		fmt.Printf("  Raw model output: %s\n", cleanPath(opts.RawOut))
	}
}

func cleanPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return p
	}
	if expanded, err := expandPath(p); err == nil {
		return expanded
	}
	return p
}

func expandPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func resolveFormsWorkspace() string {
	cfg, err := loadConfig()
	if err == nil && cfg != nil {
		if ws := strings.TrimSpace(cfg.WorkspacePath()); ws != "" {
			return ws
		}
	}
	cwd, err := os.Getwd()
	if err == nil {
		return cwd
	}
	return os.TempDir()
}

func ensureFormsPythonRuntime() (string, error) {
	workspace := resolveFormsWorkspace()
	if workspace == "" {
		return "", fmt.Errorf("unable to resolve workspace")
	}
	if err := os.MkdirAll(workspace, 0755); err != nil {
		return "", fmt.Errorf("create workspace: %w", err)
	}

	venvPython := workspaceVenvPythonPath(workspace)
	if !fileExists(venvPython) {
		if err := createWorkspaceVenv(workspace); err != nil {
			return "", err
		}
	}
	if !fileExists(venvPython) {
		return "", fmt.Errorf("python venv missing: %s", venvPython)
	}

	if err := ensurePythonPackagesInstalled(venvPython, formsPythonPackages); err != nil {
		return "", err
	}
	return venvPython, nil
}

func ensurePythonPackagesInstalled(venvPython string, packages []string) error {
	if len(packages) == 0 {
		return nil
	}
	checkArgs := []string{"-c", fmt.Sprintf("import %s", strings.Join(packages, ","))}
	if _, err := runCommandWithOutput(20*time.Second, venvPython, checkArgs...); err == nil {
		return nil
	}

	if uvPath, err := exec.LookPath("uv"); err == nil {
		args := append([]string{"pip", "install", "--python", venvPython}, packages...)
		if out, err := runCommandWithOutput(3*time.Minute, uvPath, args...); err == nil {
			_ = out
			return nil
		}
	}

	args := append([]string{"-m", "pip", "install", "--disable-pip-version-check"}, packages...)
	out, err := runCommandWithOutput(3*time.Minute, venvPython, args...)
	if err != nil {
		return fmt.Errorf("install python packages failed: %s%s", singleLine(out), pythonSetupHint(out))
	}
	return nil
}

func runFormsPython(pythonPath, action string, payload map[string]string) ([]byte, error) {
	in, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	tmpDir := os.TempDir()
	scriptFile, err := os.CreateTemp(tmpDir, "sciclaw-forms-*.py")
	if err != nil {
		return nil, fmt.Errorf("create temp script: %w", err)
	}
	scriptPath := scriptFile.Name()
	defer os.Remove(scriptPath)
	if _, err := scriptFile.WriteString(formsPythonScript); err != nil {
		scriptFile.Close()
		return nil, fmt.Errorf("write temp script: %w", err)
	}
	if err := scriptFile.Close(); err != nil {
		return nil, fmt.Errorf("close temp script: %w", err)
	}

	cmd := exec.Command(pythonPath, scriptPath, "--action", action)
	cmd.Stdin = bytes.NewReader(in)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		stdoutText := strings.TrimSpace(stdout.String())
		if stderrText == "" {
			stderrText = stdoutText
		}
		if stderrText == "" {
			stderrText = err.Error()
		}
		return nil, fmt.Errorf("python forms command failed: %s", stderrText)
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return nil, errors.New("python forms command returned no output")
	}
	return []byte(output), nil
}

const formsPythonScript = `#!/usr/bin/env python3
import argparse
import json
import os
import re
import sys
from collections import defaultdict
from datetime import datetime

import requests
from pypdf import PdfReader, PdfWriter
from pypdf.generic import NameObject, TextStringObject


def fail(msg: str, code: int = 2):
    payload = {"ok": False, "error": msg}
    sys.stderr.write(json.dumps(payload) + "\n")
    sys.exit(code)


def checkbox_value(v):
    s = str(v or "").strip().lower()
    return "Yes" if s in {"yes", "/yes", "1", "true", "on", "x", "checked"} else "Off"


def sanitize_text(v):
    out = str(v or "")
    replacements = {
        "[PATIENT]": "Alex Carter",
        "[STAFF]": "Jordan Lee",
        "[LOC]": "Cincinnati, OH",
        "[PHONE]": "513-555-0100",
        "[HOSP]": "Cincinnati Childrens Hospital",
    }
    for k, rep in replacements.items():
        out = out.replace(k, rep)
    out = out.replace("[DATE]", datetime.now().strftime("%m/%d/%Y"))
    out = re.sub(r"\s+", " ", out).strip()
    return out


def checkbox_on_name(widget):
    try:
        ap = widget.get("/AP")
        if ap is None:
            return NameObject("/Yes")
        ap = ap.get_object() if hasattr(ap, "get_object") else ap
        normal = ap.get("/N") if isinstance(ap, dict) else None
        if normal is None:
            return NameObject("/Yes")
        normal = normal.get_object() if hasattr(normal, "get_object") else normal
        if isinstance(normal, dict):
            keys = [k for k in normal.keys() if str(k) != "/Off"]
            if keys:
                return NameObject(str(keys[0]))
    except Exception:
        pass
    return NameObject("/Yes")


def verify_acroform(pdf_path):
    if not os.path.exists(pdf_path):
        raise ValueError(f"PDF not found: {pdf_path}")

    reader = PdfReader(pdf_path)
    trailer = reader.trailer
    root = trailer.get("/Root")
    if root is None:
        raise ValueError("invalid PDF: missing /Root")
    if hasattr(root, "get_object"):
        root = root.get_object()

    acro = root.get("/AcroForm") if isinstance(root, dict) else None
    if acro is None:
        raise ValueError("not an AcroForm PDF: /AcroForm dictionary is missing")
    if hasattr(acro, "get_object"):
        acro = acro.get_object()

    fields = acro.get("/Fields") if isinstance(acro, dict) else None
    if not fields or len(fields) == 0:
        raise ValueError("AcroForm found but /Fields is empty")

    field_meta = {}
    widget_count = 0
    for page_i, page in enumerate(reader.pages, start=1):
        annots = page.get("/Annots")
        if not annots:
            continue
        for ref in annots:
            obj = ref.get_object()
            if obj.get("/Subtype") != "/Widget":
                continue
            t = obj.get("/T")
            if not t:
                continue
            widget_count += 1
            name = str(t)
            if name.startswith("(") and name.endswith(")"):
                name = name[1:-1]
            ftype = "CheckBox" if obj.get("/FT") == "/Btn" else "Text"
            tooltip = str(obj.get("/TU") or "")
            if tooltip.startswith("(") and tooltip.endswith(")"):
                tooltip = tooltip[1:-1]
            if name not in field_meta:
                field_meta[name] = {
                    "name": name,
                    "type": ftype,
                    "tooltip": tooltip,
                    "pages": [page_i],
                }
            else:
                if page_i not in field_meta[name]["pages"]:
                    field_meta[name]["pages"].append(page_i)

    if widget_count == 0:
        raise ValueError("AcroForm found but no /Widget annotations were detected")

    summary = {
        "ok": True,
        "acroform": True,
        "field_count": len(field_meta),
        "widget_count": widget_count,
        "page_count": len(reader.pages),
        "fields": [field_meta[k] for k in sorted(field_meta.keys())],
    }
    return summary


def load_values_json(path):
    raw = json.load(open(path, "r", encoding="utf-8"))
    if isinstance(raw, dict) and "fields" in raw and isinstance(raw["fields"], dict):
        return {str(k): str(v) for k, v in raw["fields"].items()}
    if isinstance(raw, dict):
        return {str(k): str(v) for k, v in raw.items()}
    raise ValueError("values JSON must be an object or {\"fields\": {...}}")


def call_ollama(ollama_url, model, messages, num_predict=4096):
    base = (ollama_url or "http://localhost:11434").rstrip("/")
    url = base + "/api/chat"
    payload = {
        "model": model,
        "messages": messages,
        "stream": False,
        "format": "json",
        "think": False,
        "options": {
            "temperature": 0,
            "num_ctx": 16384,
            "num_predict": num_predict,
        },
    }
    resp = requests.post(url, json=payload, timeout=900)
    resp.raise_for_status()
    data = resp.json()
    return data.get("message", {}).get("content", "")


def parse_json_payload(text):
    text = (text or "").strip()
    if not text:
        return {}
    try:
        return json.loads(text)
    except Exception:
        m = re.search(r"\{[\s\S]*\}", text)
        if m:
            return json.loads(m.group(0))
    raise ValueError("model did not return valid JSON")


def enforce_checkbox_groups(values):
    groups = defaultdict(list)
    for key in list(values.keys()):
        m = re.match(r"^(Yes|No|Unknown|Uknown)\s*-\s*(.+)$", key, flags=re.IGNORECASE)
        if not m:
            continue
        label = m.group(1).lower()
        if label == "uknown":
            label = "unknown"
        suffix = m.group(2).strip().lower()
        groups[suffix].append((label, key))

    for _suffix, items in groups.items():
        selected = [k for label, k in items if checkbox_value(values.get(k)) == "Yes"]
        choice = None
        if len(selected) == 1:
            continue
        if len(selected) > 1:
            for pref in ["yes", "no", "unknown"]:
                for label, key in items:
                    if key in selected and label == pref:
                        choice = key
                        break
                if choice:
                    break
        else:
            for pref in ["unknown", "no", "yes"]:
                for label, key in items:
                    if label == pref:
                        choice = key
                        break
                if choice:
                    break
        for _label, key in items:
            values[key] = "Yes" if key == choice else "Off"


def apply_required_placeholders(values):
    today = datetime.now().strftime("%m/%d/%Y")
    defaults = {
        "GUARDIANSHIP OF": "Alex Carter",
        "Name  TitleProfession": "Jordan Lee, MD",
        "Business Address": "100 Main St, Cincinnati, OH 45202",
        "Business Telephone Number": "513-555-0100",
        "Dates of evaluation": today,
        "Places of evaluation": "Cincinnati Childrens Hospital",
        "Amount of time spent on evaluation": "60 minutes",
        "Length of time the individual has been your patient": "1 year",
    }
    for key, val in defaults.items():
        if not str(values.get(key, "")).strip():
            values[key] = val


def llm_fill_values(source_text, field_list, model, ollama_url):
    prompt = {
        "task": "Fill AcroForm fields from source text.",
        "rules": [
            "Return strict JSON only: {\"fields\": {\"Field Name\": \"Value\"}}",
            "Use only provided field names.",
            "For checkboxes output Yes or Off.",
            "For Yes/No/Unknown groups choose exactly one Yes.",
            "If a required identity field is absent, leave empty (CLI may synthesize if allowed).",
        ],
        "source_text": source_text,
        "fields": field_list,
    }
    raw = call_ollama(
        ollama_url,
        model,
        [
            {"role": "system", "content": "You are a legal form autofill assistant. Output strict JSON only."},
            {"role": "user", "content": json.dumps(prompt, ensure_ascii=False)},
        ],
    )
    obj = parse_json_payload(raw)
    vals = obj.get("fields", {}) if isinstance(obj, dict) else {}
    if not isinstance(vals, dict):
        vals = {}
    return {str(k): str(v) for k, v in vals.items()}, raw


def llm_backfill_values(source_text, field_list, values, model, ollama_url):
    missing = [
        {"name": f["name"], "tooltip": f.get("tooltip", "")}
        for f in field_list
        if f.get("type") == "Text" and not str(values.get(f["name"], "")).strip()
    ]
    if not missing:
        return {}, ""

    prompt = {
        "task": "Backfill missing text fields.",
        "rules": [
            "Return strict JSON only: {\"fields\": {\"Field Name\": \"Value\"}}",
            "Use only provided field names.",
            "If value is unknown, return N/A.",
        ],
        "source_text": source_text,
        "already_filled": values,
        "missing_fields": missing,
    }
    raw = call_ollama(
        ollama_url,
        model,
        [
            {"role": "system", "content": "You fill missing legal-form text fields. Output strict JSON only."},
            {"role": "user", "content": json.dumps(prompt, ensure_ascii=False)},
        ],
        num_predict=2048,
    )
    obj = parse_json_payload(raw)
    vals = obj.get("fields", {}) if isinstance(obj, dict) else {}
    if not isinstance(vals, dict):
        vals = {}
    return {str(k): str(v) for k, v in vals.items()}, raw


def write_filled_pdf(pdf_path, output_path, values):
    reader = PdfReader(pdf_path)
    writer = PdfWriter()
    writer.clone_document_from_reader(reader)
    applied = 0

    field_types = {}
    for page in writer.pages:
        annots = page.get("/Annots")
        if not annots:
            continue
        for ref in annots:
            obj = ref.get_object()
            if obj.get("/Subtype") != "/Widget":
                continue
            t = obj.get("/T")
            if not t:
                continue
            name = str(t)
            if name.startswith("(") and name.endswith(")"):
                name = name[1:-1]
            field_types[name] = "CheckBox" if obj.get("/FT") == "/Btn" else "Text"

    for page in writer.pages:
        annots = page.get("/Annots")
        if not annots:
            continue
        for ref in annots:
            obj = ref.get_object()
            if obj.get("/Subtype") != "/Widget":
                continue
            t = obj.get("/T")
            if not t:
                continue
            name = str(t)
            if name.startswith("(") and name.endswith(")"):
                name = name[1:-1]
            if name not in values:
                continue
            if field_types.get(name) == "CheckBox":
                on_name = checkbox_on_name(obj)
                state = on_name if checkbox_value(values[name]) == "Yes" else NameObject("/Off")
                obj[NameObject("/V")] = state
                obj[NameObject("/AS")] = state
                applied += 1
            else:
                obj[NameObject("/V")] = TextStringObject(str(values[name] or ""))
                applied += 1

    with open(output_path, "wb") as f:
        writer.write(f)
    return applied


def summarize(values, field_list, verify):
    text_total = sum(1 for f in field_list if f.get("type") == "Text")
    cb_total = sum(1 for f in field_list if f.get("type") == "CheckBox")
    text_nonempty = sum(1 for f in field_list if f.get("type") == "Text" and str(values.get(f["name"], "")).strip())
    cb_yes = sum(1 for f in field_list if f.get("type") == "CheckBox" and checkbox_value(values.get(f["name"], "")) == "Yes")

    return {
        "ok": True,
        "acroform": True,
        "field_count": verify["field_count"],
        "widget_count": verify["widget_count"],
        "page_count": verify["page_count"],
        "text_fields_total": text_total,
        "text_fields_nonempty": text_nonempty,
        "checkbox_fields_total": cb_total,
        "checkbox_checked_yes": cb_yes,
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--action", required=True, choices=["inspect", "fill"])
    args = parser.parse_args()

    raw_payload = sys.stdin.read()
    if not raw_payload.strip():
        fail("missing JSON payload on stdin")

    try:
        payload = json.loads(raw_payload)
    except Exception as e:
        fail(f"invalid stdin JSON payload: {e}")

    pdf_path = payload.get("pdf", "")
    if not str(pdf_path).strip():
        fail("pdf path is required")

    try:
        verify = verify_acroform(pdf_path)
    except Exception as e:
        fail(str(e))

    if args.action == "inspect":
        sys.stdout.write(json.dumps(verify))
        return

    out_path = payload.get("out", "")
    if not str(out_path).strip():
        fail("out path is required for fill")

    source_path = payload.get("source", "")
    values_path = payload.get("values", "")
    if bool(source_path) == bool(values_path):
        fail("provide exactly one of source or values")

    model = payload.get("model", "qwen3.5:9b")
    ollama_url = payload.get("ollama_url", "http://localhost:11434")
    no_synthetic = str(payload.get("no_synthetic", "false")).lower() == "true"
    skip_backfill = str(payload.get("skip_backfill", "false")).lower() == "true"

    field_list = verify["fields"]
    field_map = {f["name"]: f for f in field_list}
    values = {}
    raw_primary = ""
    raw_backfill = ""
    source_mode = False

    if values_path:
        try:
            values = load_values_json(values_path)
        except Exception as e:
            fail(f"invalid values JSON: {e}")
    else:
        source_mode = True
        if not os.path.exists(source_path):
            fail(f"source text file not found: {source_path}")
        source_text = open(source_path, "r", encoding="utf-8").read()[:20000]
        try:
            values, raw_primary = llm_fill_values(source_text, field_list, model, ollama_url)
            if not skip_backfill:
                add, raw_backfill = llm_backfill_values(source_text, field_list, values, model, ollama_url)
                values.update(add)
        except Exception as e:
            fail(f"LLM fill failed: {e}")

    # Keep only known fields.
    values = {k: v for k, v in values.items() if k in field_map}

    if source_mode:
        # Normalize all fields for generated mode so tri-state checkboxes and
        # required fields are always represented.
        for field in field_list:
            name = field["name"]
            if field.get("type") == "CheckBox":
                values[name] = checkbox_value(values.get(name, "Off"))
            else:
                values[name] = sanitize_text(values.get(name, ""))
        enforce_checkbox_groups(values)
        if not no_synthetic:
            apply_required_placeholders(values)
    else:
        # Direct values mode: only touch fields explicitly provided.
        for name in list(values.keys()):
            field = field_map.get(name)
            if not field:
                continue
            if field.get("type") == "CheckBox":
                values[name] = checkbox_value(values.get(name, "Off"))
            else:
                values[name] = sanitize_text(values.get(name, ""))

    try:
        os.makedirs(os.path.dirname(os.path.abspath(out_path)), exist_ok=True)
        applied = write_filled_pdf(pdf_path, out_path, values)
        if applied == 0:
            fail("no values matched form fields; verify field names with forms inspect")
    except Exception as e:
        fail(f"write filled PDF failed: {e}")

    summary = summarize(values, field_list, verify)

    json_out = payload.get("json_out", "")
    if json_out:
        with open(json_out, "w", encoding="utf-8") as f:
            json.dump({"summary": summary, "fields": values}, f, indent=2, ensure_ascii=False)

    raw_out = payload.get("raw_out", "")
    if raw_out:
        with open(raw_out, "w", encoding="utf-8") as f:
            if raw_primary:
                f.write(raw_primary)
            if raw_backfill:
                f.write("\n\n--- backfill ---\n\n")
                f.write(raw_backfill)

    sys.stdout.write(json.dumps(summary))


if __name__ == "__main__":
    main()
`
