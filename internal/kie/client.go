package kie

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/digkill/TGStickerBot/internal/config"
)

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	log        *slog.Logger
}

type GenerateOptions struct {
	Prompt       string
	AspectRatio  string
	Resolution   string
	InputURLs    []string
	OutputFormat string
}

type Image struct {
	URL   string
	Bytes []byte
	Mime  string
}

func NewClient(cfg config.Config, log *slog.Logger) *Client {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute // Увеличиваем таймаут для асинхронных запросов
	}

	trimmedBase := strings.TrimRight(cfg.KIEBaseURL, "/")
	return &Client{
		apiKey:  cfg.KIEAPIKey,
		baseURL: trimmedBase,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		log: log,
	}
}

func (c *Client) GenerateFlux2(ctx context.Context, opts GenerateOptions) (*Image, error) {
	// Flux 2 использует асинхронный API
	// Определяем тип модели: если есть input_urls, используем image-to-image, иначе text-to-image
	// Примечание: если flux-2/pro-text-to-image не существует, может потребоваться другой идентификатор модели
	modelName := "flux-2/pro-text-to-image"
	if len(opts.InputURLs) > 0 {
		modelName = "flux-2/pro-image-to-image"
	}

	input := map[string]any{
		"prompt":       opts.Prompt,
		"aspect_ratio": opts.AspectRatio,
		"resolution":   opts.Resolution,
	}
	if len(opts.InputURLs) > 0 {
		input["input_urls"] = opts.InputURLs
	}

	requestBody := map[string]any{
		"model": modelName,
		"input": input,
	}

	return c.postAsync(ctx, requestBody)
}

func (c *Client) GenerateNanoBanana(ctx context.Context, opts GenerateOptions) (*Image, error) {
	// Nano Banana Pro использует асинхронный API
	requestBody := map[string]any{
		"model": "nano-banana-pro",
		"input": map[string]any{
			"prompt":       opts.Prompt,
			"aspect_ratio": opts.AspectRatio,
			"resolution":   opts.Resolution,
			"output_format": func() string {
				if opts.OutputFormat != "" {
					return strings.ToLower(opts.OutputFormat)
				}
				return "png"
			}(),
		},
	}
	if len(opts.InputURLs) > 0 {
		requestBody["input"].(map[string]any)["image_input"] = opts.InputURLs
	}

	return c.postAsync(ctx, requestBody)
}

// postAsync создает задачу и опрашивает статус до завершения
func (c *Client) postAsync(ctx context.Context, payload map[string]any) (*Image, error) {
	// Шаг 1: Создать задачу
	taskID, err := c.createTask(ctx, payload)
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	// Шаг 2: Опрашивать статус
	return c.pollTaskStatus(ctx, taskID)
}

// createTask создает задачу и возвращает taskId
func (c *Client) createTask(ctx context.Context, payload map[string]any) (string, error) {
	// Правильно объединяем URL
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}
	endpoint, err := url.Parse("/api/v1/jobs/createTask")
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	fullURL := baseURL.ResolveReference(endpoint).String()

	if c.log != nil {
		c.log.Info("creating KIE task", "url", fullURL, "model", getModelFromPayload(payload))
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("post kie: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 300 {
		if c.log != nil {
			c.log.Error("KIE create task failed", "status", resp.StatusCode, "url", fullURL, "body", truncateBody(rawBody))
		}
		return "", fmt.Errorf("kie error: status=%d url=%s body=%s", resp.StatusCode, fullURL, truncateBody(rawBody))
	}

	var createResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			TaskID string `json:"taskId"`
		} `json:"data"`
	}

	if err := json.Unmarshal(rawBody, &createResp); err != nil {
		return "", fmt.Errorf("decode create task response: %w (body=%s)", err, truncateBody(rawBody))
	}

	if createResp.Code != 200 {
		return "", fmt.Errorf("create task failed: code=%d msg=%s", createResp.Code, createResp.Msg)
	}

	if createResp.Data.TaskID == "" {
		return "", fmt.Errorf("empty taskId in response")
	}

	if c.log != nil {
		c.log.Info("KIE task created", "task_id", createResp.Data.TaskID)
	}

	return createResp.Data.TaskID, nil
}

// pollTaskStatus опрашивает статус задачи до завершения
func (c *Client) pollTaskStatus(ctx context.Context, taskID string) (*Image, error) {
	// Правильно объединяем URL
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	endpoint, err := url.Parse("/api/v1/jobs/recordInfo")
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	params := url.Values{}
	params.Set("taskId", taskID)
	endpoint.RawQuery = params.Encode()
	fullURL := baseURL.ResolveReference(endpoint).String()

	maxAttempts := 60
	pollInterval := 2 * time.Second

	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return nil, fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("get task status: %w", err)
		}

		rawBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response body: %w", err)
		}

		if resp.StatusCode >= 300 {
			if c.log != nil {
				c.log.Error("KIE poll task status failed", "status", resp.StatusCode, "url", fullURL, "body", truncateBody(rawBody))
			}
			return nil, fmt.Errorf("kie error: status=%d url=%s body=%s", resp.StatusCode, fullURL, truncateBody(rawBody))
		}

		var statusResp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				TaskID     string `json:"taskId"`
				State      string `json:"state"`
				ResultJSON string `json:"resultJson"`
				FailCode   string `json:"failCode"`
				FailMsg    string `json:"failMsg"`
			} `json:"data"`
		}

		if err := json.Unmarshal(rawBody, &statusResp); err != nil {
			return nil, fmt.Errorf("decode status response: %w (body=%s)", err, truncateBody(rawBody))
		}

		if statusResp.Code != 200 {
			return nil, fmt.Errorf("get task status failed: code=%d msg=%s", statusResp.Code, statusResp.Msg)
		}

		state := statusResp.Data.State
		switch state {
		case "success":
			// Извлекаем результат
			if statusResp.Data.ResultJSON == "" {
				return nil, fmt.Errorf("empty resultJson in success response")
			}

			var result struct {
				ResultURLs []string `json:"resultUrls"`
			}
			if err := json.Unmarshal([]byte(statusResp.Data.ResultJSON), &result); err != nil {
				return nil, fmt.Errorf("parse resultJson: %w", err)
			}

			if len(result.ResultURLs) == 0 {
				return nil, fmt.Errorf("no resultUrls in result")
			}

			if c.log != nil {
				c.log.Info("KIE task completed", "task_id", taskID, "attempt", attempt+1)
			}

			return &Image{URL: result.ResultURLs[0]}, nil

		case "fail":
			failMsg := statusResp.Data.FailMsg
			if failMsg == "" {
				failMsg = "unknown error"
			}
			if c.log != nil {
				c.log.Error("KIE task failed", "task_id", taskID, "fail_code", statusResp.Data.FailCode, "fail_msg", failMsg)
			}
			return nil, fmt.Errorf("task failed: %s (code: %s)", failMsg, statusResp.Data.FailCode)

		case "waiting", "generating", "processing", "queued", "queueing":
			// Продолжаем опрос
			if c.log != nil && attempt%10 == 0 { // Логируем каждые 10 попыток
				c.log.Info("KIE task waiting", "task_id", taskID, "attempt", attempt+1, "max_attempts", maxAttempts)
			}
			if attempt < maxAttempts-1 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(pollInterval):
					continue
				}
			}
			return nil, fmt.Errorf("task timeout after %d attempts", maxAttempts)

		default:
			return nil, fmt.Errorf("unknown task state: %s", state)
		}
	}

	return nil, fmt.Errorf("task timeout after %d attempts", maxAttempts)
}

func getModelFromPayload(payload map[string]any) string {
	if model, ok := payload["model"].(string); ok {
		return model
	}
	return "unknown"
}

func truncateBody(body []byte) string {
	const limit = 512
	s := strings.TrimSpace(string(body))
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
