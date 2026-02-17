package chatbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type ChatbotReplier struct {
}

func NewChatbotReplier() *ChatbotReplier {
	return &ChatbotReplier{}
}

func (r *ChatbotReplier) SimpleReplyText(ctx context.Context, sessionWebhook string, content []byte) error {
	requestBody := map[string]interface{}{
		"msgtype": "text",
		"text": map[string]interface{}{
			"content": string(content),
		},
	}
	return r.ReplyMessage(ctx, sessionWebhook, requestBody)
}

func (r *ChatbotReplier) SimpleReplyMarkdown(ctx context.Context, sessionWebhook string, title, content []byte) error {
	requestBody := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]interface{}{
			"title": string(title),
			"text":  string(content),
		},
	}
	return r.ReplyMessage(ctx, sessionWebhook, requestBody)
}

func (r *ChatbotReplier) ReplyMessage(ctx context.Context, sessionWebhook string, requestBody map[string]interface{}) error {
	requestJsonBody, _ := json.Marshal(requestBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sessionWebhook, bytes.NewReader(requestJsonBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")

	httpClient := &http.Client{
		Transport: http.DefaultTransport,
		Timeout:   5 * time.Second,
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		defer resp.Body.Close()

		responseJsonBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		return fmt.Errorf(string(responseJsonBody))
	}

	return nil
}
