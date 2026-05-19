package protocol

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"

	"chatgpt2api/internal/service"
	"chatgpt2api/internal/util"
)

type RelayConfigProvider interface {
	RelayEnabled() bool
	RelayBaseURL() string
	RelayAPIKey() string
	RelayModel() string
	RelayTimeoutSeconds() int
}

func (e *Engine) StreamImageOutputsWithRelay(ctx context.Context, request ConversationRequest) (<-chan ImageOutput, <-chan error) {
	request = request.Normalized()
	out := make(chan ImageOutput)
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)

		if e.RelayConfig == nil || !e.RelayConfig.RelayEnabled() {
			errCh <- NewImageGenerationError("relay mode is not enabled")
			return
		}
		baseURL := strings.TrimRight(strings.TrimSpace(e.RelayConfig.RelayBaseURL()), "/")
		if baseURL == "" {
			errCh <- NewImageGenerationError("relay base URL is not configured")
			return
		}
		apiKey := strings.TrimSpace(e.RelayConfig.RelayAPIKey())
		if apiKey == "" {
			errCh <- NewImageGenerationError("relay api key is not configured")
			return
		}

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		resultCh := make(chan imageRunResult, request.N)
		var wg sync.WaitGroup
		for index := 1; index <= request.N; index++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				releaseSlot, err := request.acquireImageOutputSlot(ctx, index)
				if err != nil {
					cancel()
					resultCh <- imageRunResult{lastError: err.Error(), err: err}
					return
				}
				defer releaseSlot()
				result := e.runRelayImageOutput(ctx, out, request, baseURL, apiKey, index)
				if result.err != nil {
					cancel()
				}
				resultCh <- result
			}(index)
		}
		go func() {
			wg.Wait()
			close(resultCh)
		}()

		emittedAny := false
		lastError := ""
		for result := range resultCh {
			emittedAny = emittedAny || result.emitted
			if result.lastError != "" {
				lastError = result.lastError
			}
			if result.err != nil {
				errCh <- result.err
				return
			}
		}
		if !emittedAny {
			errCh <- NewImageGenerationError(imageStreamErrorMessage(lastError))
			return
		}
		errCh <- nil
	}()

	return out, errCh
}

func (e *Engine) runRelayImageOutput(ctx context.Context, out chan<- ImageOutput, request ConversationRequest, baseURL, apiKey string, index int) imageRunResult {
	result := imageRunResult{}

	model := relayResolveModel(request.Model, e.RelayConfig.RelayModel())
	timeout := time.Duration(e.RelayConfig.RelayTimeoutSeconds()) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	httpClient := &http.Client{Timeout: timeout}

	var (
		req *http.Request
		err error
	)
	if len(request.Images) > 0 {
		req, err = relayBuildEditRequest(ctx, baseURL, apiKey, model, request)
	} else {
		req, err = relayBuildGenerationRequest(ctx, baseURL, apiKey, model, request)
	}
	if err != nil {
		result.err = NewImageGenerationError(err.Error())
		result.lastError = err.Error()
		return result
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		msg := fmt.Sprintf("relay request failed: %s", err.Error())
		result.err = NewImageGenerationError(msg)
		result.lastError = msg
		return result
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		msg := fmt.Sprintf("relay response read failed: %s", err.Error())
		result.err = NewImageGenerationError(msg)
		result.lastError = msg
		return result
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := relayParseErrorMessage(body)
		if msg == "" {
			msg = fmt.Sprintf("relay returned status %d", resp.StatusCode)
		}
		if e.Logger != nil {
			e.Logger.Warning(fmt.Sprintf("relay upstream error: status=%d body=%s", resp.StatusCode, truncateString(string(body), 500)))
		}
		result.err = &ImageGenerationError{Message: msg, StatusCode: resp.StatusCode, Type: "server_error", Code: "upstream_error"}
		result.lastError = msg
		return result
	}

	var parsed struct {
		Created int64 `json:"created"`
		Data    []struct {
			B64JSON       string `json:"b64_json"`
			URL           string `json:"url"`
			RevisedPrompt string `json:"revised_prompt"`
			OutputFormat  string `json:"output_format"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		msg := fmt.Sprintf("relay response parse failed: %s", err.Error())
		result.err = NewImageGenerationError(msg)
		result.lastError = msg
		return result
	}
	if len(parsed.Data) == 0 {
		msg := "relay returned no image data"
		result.err = NewImageGenerationError(msg)
		result.lastError = msg
		return result
	}

	created := parsed.Created
	if created == 0 {
		created = time.Now().Unix()
	}

	options := ImageOutputOptions{Format: NormalizeImageOutputFormat(request.OutputFormat)}
	if request.OutputCompression != nil {
		c := *request.OutputCompression
		options.Compression = &c
	}

	for _, item := range parsed.Data {
		b64 := strings.TrimSpace(item.B64JSON)
		if b64 == "" && item.URL != "" {
			fetched, fetchErr := relayFetchImage(ctx, item.URL, apiKey, timeout)
			if fetchErr != nil {
				msg := fmt.Sprintf("relay image fetch failed: %s", fetchErr.Error())
				result.err = NewImageGenerationError(msg)
				result.lastError = msg
				return result
			}
			b64 = base64.StdEncoding.EncodeToString(fetched)
		}
		if b64 == "" {
			continue
		}

		responseFormat := strings.TrimSpace(request.ResponseFormat)
		items := []map[string]any{{
			"b64_json":       b64,
			"revised_prompt": firstNonEmpty(item.RevisedPrompt, request.Prompt),
		}}
		if item.OutputFormat != "" {
			items[0]["output_format"] = item.OutputFormat
		}

		charged := false
		var charge func() error
		if request.ChargeImageOutput != nil {
			charge = func() error {
				if charged {
					return nil
				}
				charged = true
				return request.ChargeImageOutput(index)
			}
		}

		formatted, formatErr := e.FormatImageResultWithCharge(items, request.Prompt, responseFormat, request.BaseURL, request.OwnerID, request.OwnerName, created, "", options, charge)
		if formatErr != nil {
			var billingErr service.BillingLimitError
			if errors.As(formatErr, &billingErr) {
				result.err = billingErr
			} else {
				result.err = NewImageGenerationError(formatErr.Error())
			}
			result.lastError = formatErr.Error()
			return result
		}

		data, _ := formatted["data"].([]map[string]any)
		if len(data) == 0 {
			continue
		}

		select {
		case out <- ImageOutput{
			Kind:          "result",
			Model:         request.Model,
			Index:         index,
			Total:         request.N,
			Created:       created,
			Data:          data,
			ChargeHandled: true,
		}:
			result.emitted = true
		case <-ctx.Done():
			result.err = ctx.Err()
			result.lastError = ctx.Err().Error()
			return result
		}
	}

	return result
}

func relayResolveModel(requestModel, configuredModel string) string {
	configured := strings.TrimSpace(configuredModel)
	if configured != "" {
		return configured
	}
	model := strings.TrimSpace(requestModel)
	switch model {
	case "", util.ImageModelAuto, util.ImageModelGPT, util.ImageModelCodex:
		return "gpt-image-2"
	}
	return model
}

func relayBuildGenerationRequest(ctx context.Context, baseURL, apiKey, model string, request ConversationRequest) (*http.Request, error) {
	payload := map[string]any{
		"model":  model,
		"prompt": request.Prompt,
		"n":      1,
	}
	if size := strings.TrimSpace(request.Size); size != "" && size != "auto" {
		payload["size"] = size
	}
	if quality := strings.TrimSpace(request.Quality); quality != "" {
		payload["quality"] = quality
	}
	payload["response_format"] = "b64_json"

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode relay request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/images/generations", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func relayBuildEditRequest(ctx context.Context, baseURL, apiKey, model string, request ConversationRequest) (*http.Request, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("model", model); err != nil {
		return nil, err
	}
	if err := writer.WriteField("prompt", request.Prompt); err != nil {
		return nil, err
	}
	if err := writer.WriteField("n", "1"); err != nil {
		return nil, err
	}
	if size := strings.TrimSpace(request.Size); size != "" && size != "auto" {
		if err := writer.WriteField("size", size); err != nil {
			return nil, err
		}
	}
	if quality := strings.TrimSpace(request.Quality); quality != "" {
		if err := writer.WriteField("quality", quality); err != nil {
			return nil, err
		}
	}
	if err := writer.WriteField("response_format", "b64_json"); err != nil {
		return nil, err
	}

	for i, encoded := range request.Images {
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode reference image %d: %w", i, err)
		}
		filename := "image_" + strconv.Itoa(i) + ".png"
		fieldName := "image"
		if len(request.Images) > 1 {
			fieldName = "image[]"
		}
		partHeader := make(textproto.MIMEHeader)
		partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, filename))
		partHeader.Set("Content-Type", "image/png")
		part, err := writer.CreatePart(partHeader)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(raw); err != nil {
			return nil, err
		}
	}

	if mask := strings.TrimSpace(request.InputImageMask); mask != "" {
		raw, err := base64.StdEncoding.DecodeString(mask)
		if err == nil && len(raw) > 0 {
			maskHeader := make(textproto.MIMEHeader)
			maskHeader.Set("Content-Disposition", `form-data; name="mask"; filename="mask.png"`)
			maskHeader.Set("Content-Type", "image/png")
			part, err := writer.CreatePart(maskHeader)
			if err != nil {
				return nil, err
			}
			if _, err := part.Write(raw); err != nil {
				return nil, err
			}
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/images/edits", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func relayFetchImage(ctx context.Context, imageURL, apiKey string, timeout time.Duration) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func relayParseErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if envelope.Error.Message != "" {
			return envelope.Error.Message
		}
		if envelope.Message != "" {
			return envelope.Message
		}
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 500 {
		text = text[:500]
	}
	return text
}
