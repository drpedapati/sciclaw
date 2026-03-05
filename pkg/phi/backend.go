package phi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// BackendStatus reports the health of a local inference backend.
type BackendStatus struct {
	Installed  bool   `json:"installed"`
	Running    bool   `json:"running"`
	Version    string `json:"version,omitempty"`
	ModelReady bool   `json:"model_ready"`
	Error      string `json:"error,omitempty"`
}

// CheckBackend probes the given backend and returns its status.
func CheckBackend(backend string) BackendStatus {
	switch backend {
	case "ollama":
		return checkOllama()
	case "mlx":
		return BackendStatus{Error: "MLX support coming soon"}
	default:
		return BackendStatus{Error: fmt.Sprintf("unknown backend: %s", backend)}
	}
}

func checkOllama() BackendStatus {
	status := BackendStatus{}

	// Check if ollama is installed
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ollama", "--version").CombinedOutput()
	if err != nil {
		status.Error = "ollama is not installed. Install from https://ollama.com"
		return status
	}
	status.Installed = true
	status.Version = strings.TrimSpace(string(out))

	// Check if ollama is running via API
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err != nil {
		status.Error = "ollama is installed but not running"
		return status
	}
	defer resp.Body.Close()
	status.Running = true

	return status
}

// CheckModelReady returns true if the specified model tag is already pulled in Ollama.
func CheckModelReady(ollamaTag string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false
	}

	// Ollama may store tags with or without ":latest" suffix
	normalizedTag := ollamaTag
	for _, m := range result.Models {
		name := m.Name
		if name == normalizedTag || name == normalizedTag+":latest" ||
			strings.TrimSuffix(name, ":latest") == strings.TrimSuffix(normalizedTag, ":latest") {
			return true
		}
	}
	return false
}

// PullModel pulls a model using ollama pull. It calls the progress callback
// with status lines from ollama's output.
func PullModel(ctx context.Context, ollamaTag string, progress func(string)) error {
	pullCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(pullCtx, "ollama", "pull", ollamaTag)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // merge stderr into stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting ollama pull: %w", err)
	}

	buf := make([]byte, 4096)
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 && progress != nil {
			progress(strings.TrimSpace(string(buf[:n])))
		}
		if readErr != nil {
			break
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ollama pull failed: %w", err)
	}
	return nil
}

// WarmupModel sends a trivial prompt to verify the model responds correctly.
func WarmupModel(ctx context.Context, ollamaTag string) error {
	warmupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"max_tokens":16}`, ollamaTag)
	req, err := http.NewRequestWithContext(warmupCtx, "POST",
		"http://localhost:11434/v1/chat/completions",
		strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating warmup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("warmup request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("warmup returned status %d", resp.StatusCode)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("parsing warmup response: %w", err)
	}
	if len(result.Choices) == 0 {
		return fmt.Errorf("warmup returned no choices")
	}

	return nil
}

// OllamaListModels returns the list of locally available model tags.
func OllamaListModels() ([]string, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err != nil {
		return nil, fmt.Errorf("connecting to ollama: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parsing ollama response: %w", err)
	}

	models := make([]string, len(result.Models))
	for i, m := range result.Models {
		models[i] = m.Name
	}
	return models, nil
}
