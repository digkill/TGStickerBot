package kie

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/stickerbot/internal/config"
)

type Client struct {
	apiKey     string
	flux2URL   string
	nanoURL    string
	httpClient *http.Client
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

func NewClient(cfg config.Config) *Client {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = time.Minute
	}

	trimmedBase := strings.TrimRight(cfg.KIEBaseURL, "/")
	return &Client{
		apiKey:   cfg.KIEAPIKey,
		flux2URL: trimmedBase + cfg.Flux2Path,
		nanoURL:  trimmedBase + cfg.NanoBananaPath,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) GenerateFlux2(ctx context.Context, opts GenerateOptions) (*Image, error) {
	requestBody := map[string]any{
		"prompt":       opts.Prompt,
		"aspect_ratio": opts.AspectRatio,
		"resolution":   opts.Resolution,
	}
	if len(opts.InputURLs) > 0 {
		requestBody["input_urls"] = opts.InputURLs
	}
	return c.post(ctx, c.flux2URL, requestBody)
}

func (c *Client) GenerateNanoBanana(ctx context.Context, opts GenerateOptions) (*Image, error) {
	requestBody := map[string]any{
		"prompt":       opts.Prompt,
		"aspect_ratio": opts.AspectRatio,
		"resolution":   opts.Resolution,
		"output_format": func() string {
			if opts.OutputFormat != "" {
				return strings.ToLower(opts.OutputFormat)
			}
			return "png"
		}(),
	}
	if len(opts.InputURLs) > 0 {
		requestBody["image_input"] = opts.InputURLs
	}
	return c.post(ctx, c.nanoURL, requestBody)
}

func (c *Client) post(ctx context.Context, url string, payload map[string]any) (*Image, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post kie: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("kie error: status=%d body=%s", resp.StatusCode, string(raw))
	}

	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	img, err := apiResp.FirstImage()
	if err != nil {
		return nil, err
	}
	return img, nil
}

type apiResponse struct {
	Output []apiImage `json:"output"`
	Data   apiImage   `json:"data"`
	Result apiImage   `json:"result"`
}

type apiImage struct {
	ImageURL    string `json:"image_url"`
	URL         string `json:"url"`
	ImageBase64 string `json:"image_base64"`
	Data        string `json:"data"`
	Mime        string `json:"mime"`
}

func (r *apiResponse) FirstImage() (*Image, error) {
	candidates := make([]apiImage, 0, len(r.Output)+2)
	candidates = append(candidates, r.Output...)
	if r.Data.ImageURL != "" || r.Data.ImageBase64 != "" {
		candidates = append(candidates, r.Data)
	}
	if r.Result.ImageURL != "" || r.Result.ImageBase64 != "" {
		candidates = append(candidates, r.Result)
	}

	for _, candidate := range candidates {
		if candidate.ImageURL != "" {
			return &Image{URL: candidate.ImageURL, Mime: candidate.Mime}, nil
		}
		if candidate.URL != "" {
			return &Image{URL: candidate.URL, Mime: candidate.Mime}, nil
		}
		if candidate.ImageBase64 != "" {
			data, err := base64.StdEncoding.DecodeString(candidate.ImageBase64)
			if err != nil {
				continue
			}
			return &Image{Bytes: data, Mime: candidate.Mime}, nil
		}
		if candidate.Data != "" {
			data, err := base64.StdEncoding.DecodeString(candidate.Data)
			if err != nil {
				continue
			}
			return &Image{Bytes: data, Mime: candidate.Mime}, nil
		}
	}

	return nil, fmt.Errorf("kie response does not contain image")
}
