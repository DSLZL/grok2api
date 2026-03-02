package reverse

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const chatAPIPath = "/rest/app-chat/conversations/new"

type AppChatConfig struct {
	DisableMemory bool
	Temporary     bool
	ChatTimeout   time.Duration
	VideoTimeout  time.Duration
	ImageTimeout  time.Duration
	Retry         RetryPolicy
}

func DefaultAppChatConfig() AppChatConfig {
	return AppChatConfig{
		DisableMemory: true,
		Temporary:     true,
		ChatTimeout:   60 * time.Second,
		VideoTimeout:  60 * time.Second,
		ImageTimeout:  60 * time.Second,
		Retry:         DefaultRetryPolicy(),
	}
}

type AppChatRequest struct {
	Message             string
	Model               string
	Mode                string
	FileAttachments     []string
	ToolOverrides       map[string]any
	ModelConfigOverride map[string]any
}

type AppChatAdapter struct {
	endpoint string
	client   *http.Client
	config   AppChatConfig
}

func NewAppChatAdapter(baseURL string, client *http.Client, cfg AppChatConfig) *AppChatAdapter {
	trimmed := strings.TrimRight(baseURL, "/")
	if client == nil {
		client = &http.Client{}
	}
	return &AppChatAdapter{
		endpoint: trimmed + chatAPIPath,
		client:   client,
		config:   cfg,
	}
}

func NormalizeChatProxy(proxyURL string) string {
	if proxyURL == "" {
		return ""
	}
	normalized := strings.TrimSpace(proxyURL)
	lower := strings.ToLower(normalized)
	if strings.HasPrefix(lower, "socks5://") {
		return "socks5h://" + normalized[len("socks5://"):]
	}
	if strings.HasPrefix(lower, "socks4://") {
		return "socks4a://" + normalized[len("socks4://"):]
	}
	return normalized
}

func BuildAppChatPayload(req AppChatRequest, cfg AppChatConfig) map[string]any {
	attachments := req.FileAttachments
	if attachments == nil {
		attachments = []string{}
	}
	toolOverrides := req.ToolOverrides
	if toolOverrides == nil {
		toolOverrides = map[string]any{}
	}

	payload := map[string]any{
		"deviceEnvInfo": map[string]any{
			"darkModeEnabled":  false,
			"devicePixelRatio": 2,
			"screenWidth":      2056,
			"screenHeight":     1329,
			"viewportWidth":    2056,
			"viewportHeight":   1083,
		},
		"disableMemory":               cfg.DisableMemory,
		"disableSearch":               false,
		"disableSelfHarmShortCircuit": false,
		"disableTextFollowUps":        false,
		"enableImageGeneration":       true,
		"enableImageStreaming":        true,
		"enableSideBySide":            true,
		"fileAttachments":             attachments,
		"forceConcise":                false,
		"forceSideBySide":             false,
		"imageAttachments":            []string{},
		"imageGenerationCount":        2,
		"isAsyncChat":                 false,
		"isReasoning":                 false,
		"message":                     req.Message,
		"modelMode":                   req.Mode,
		"modelName":                   req.Model,
		"responseMetadata": map[string]any{
			"requestModelDetails": map[string]any{"modelId": req.Model},
		},
		"returnImageBytes":          false,
		"returnRawGrokInXaiRequest": false,
		"sendFinalMetadata":         true,
		"temporary":                 cfg.Temporary,
		"toolOverrides":             toolOverrides,
	}

	if len(req.ModelConfigOverride) > 0 {
		responseMetadata := payload["responseMetadata"].(map[string]any)
		responseMetadata["modelConfigOverride"] = req.ModelConfigOverride
	}

	return payload
}

func (a *AppChatAdapter) Stream(ctx context.Context, token string, req AppChatRequest, onLine func(line string) error) error {
	if onLine == nil {
		return fmt.Errorf("stream callback is nil")
	}
	payloadBytes, err := json.Marshal(BuildAppChatPayload(req, a.config))
	if err != nil {
		return err
	}

	requestTimeout := a.resolveTimeout()
	if requestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, requestTimeout)
		defer cancel()
	}

	var response *http.Response
	retryErr := RetryOnError(ctx, a.config.Retry, nil, func() error {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
			response = nil
		}

		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(payloadBytes))
		if reqErr != nil {
			return reqErr
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "*/*")
		httpReq.Header.Set("Origin", "https://grok.com")
		httpReq.Header.Set("Referer", "https://grok.com/")
		if strings.TrimSpace(token) != "" {
			httpReq.Header.Set("Cookie", normalizeCookieToken(token))
		}

		resp, doErr := a.client.Do(httpReq)
		if doErr != nil {
			return &ReverseError{Status: http.StatusBadGateway, Code: ErrorCodeConnectionFailed, Message: doErr.Error()}
		}
		if resp.StatusCode != http.StatusOK {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			return MapHTTPError(resp.StatusCode, string(body), resp.Header)
		}
		response = resp
		return nil
	})
	if retryErr != nil {
		return retryErr
	}
	if response == nil || response.Body == nil {
		return &ReverseError{Status: http.StatusBadGateway, Code: ErrorCodeUpstreamServerError, Message: "empty upstream response"}
	}
	defer response.Body.Close()

	reader := bufio.NewReader(response.Body)
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			if callbackErr := onLine(line); callbackErr != nil {
				return callbackErr
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return &ReverseError{Status: http.StatusBadGateway, Code: ErrorCodeUpstreamServerError, Message: readErr.Error()}
		}
	}
}

func (a *AppChatAdapter) resolveTimeout() time.Duration {
	if a.config.ChatTimeout > 0 {
		return a.config.ChatTimeout
	}
	timeout := a.config.VideoTimeout
	if a.config.ImageTimeout > timeout {
		timeout = a.config.ImageTimeout
	}
	return timeout
}

func normalizeCookieToken(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "sso=") {
		return trimmed
	}
	return "sso=" + trimmed
}
