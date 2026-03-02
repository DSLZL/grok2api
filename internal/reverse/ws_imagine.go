package reverse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

type WSMessageType string

const (
	WSMessageTypeImage   WSMessageType = "image"
	WSMessageTypeError   WSMessageType = "error"
	WSMessageTypeClose   WSMessageType = "close"
	WSMessageTypeTimeout WSMessageType = "timeout"
)

type WSMessage struct {
	Type        WSMessageType
	URL         string
	Blob        string
	ErrorCode   string
	Error       string
	CloseCode   int
	CloseReason string
}

type WSMessageSource interface {
	Receive(ctx context.Context) (WSMessage, error)
	Close() error
}

type WSDialFunc func(ctx context.Context) (WSMessageSource, error)

type WSImagineConfig struct {
	Timeout        time.Duration
	StreamTimeout  time.Duration
	FinalTimeout   time.Duration
	BlockedGrace   time.Duration
	MediumMinBytes int
	FinalMinBytes  int
	MaxRetries     int
}

func DefaultWSImagineConfig() WSImagineConfig {
	return WSImagineConfig{
		Timeout:        60 * time.Second,
		StreamTimeout:  60 * time.Second,
		FinalTimeout:   15 * time.Second,
		BlockedGrace:   10 * time.Second,
		MediumMinBytes: 30000,
		FinalMinBytes:  100000,
		MaxRetries:     1,
	}
}

type ImagineItem struct {
	Type     string
	ImageID  string
	Ext      string
	Stage    string
	Blob     string
	BlobSize int
	URL      string
	IsFinal  bool
}

type WSImagineAdapter struct {
	config WSImagineConfig
	dial   WSDialFunc
}

func NewWSImagineAdapter(cfg WSImagineConfig, dial WSDialFunc) *WSImagineAdapter {
	if cfg.MaxRetries < 1 {
		cfg.MaxRetries = 1
	}
	if cfg.MediumMinBytes <= 0 {
		cfg.MediumMinBytes = 30000
	}
	if cfg.FinalMinBytes <= 0 {
		cfg.FinalMinBytes = 100000
	}
	return &WSImagineAdapter{config: cfg, dial: dial}
}

func (a *WSImagineAdapter) Stream(ctx context.Context, n int, onItem func(ImagineItem) error) error {
	if a == nil || a.dial == nil {
		return &ReverseError{Status: 502, Code: ErrorCodeConnectionFailed, Message: "ws dialer is nil"}
	}
	if n < 1 {
		n = 1
	}

	var lastErr error
	for attempt := 1; attempt <= a.config.MaxRetries; attempt++ {
		err := a.streamOnce(ctx, n, onItem)
		if err == nil {
			return nil
		}
		lastErr = err
		revErr, ok := err.(*ReverseError)
		if !ok || !isRetryableWSError(revErr) || attempt == a.config.MaxRetries {
			return err
		}
	}
	return lastErr
}

func (a *WSImagineAdapter) streamOnce(ctx context.Context, n int, onItem func(ImagineItem) error) error {
	source, err := a.dial(ctx)
	if err != nil {
		return &ReverseError{Status: 502, Code: ErrorCodeConnectionFailed, Message: err.Error()}
	}
	defer source.Close()

	completed := 0
	sawMedium := false
	for {
		msg, recvErr := source.Receive(ctx)
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				if sawMedium && completed == 0 {
					return &ReverseError{Status: 403, Code: ErrorCodeBlocked, Message: "blocked_no_final_image"}
				}
				if completed >= n {
					return nil
				}
				return &ReverseError{Status: 502, Code: ErrorCodeWsClosed, Message: "websocket closed"}
			}
			return &ReverseError{Status: 502, Code: ErrorCodeConnectionFailed, Message: recvErr.Error()}
		}

		switch msg.Type {
		case WSMessageTypeImage:
			item, ok := classifyImage(msg.URL, msg.Blob, a.config.FinalMinBytes, a.config.MediumMinBytes)
			if !ok {
				continue
			}
			if item.Stage == "medium" {
				sawMedium = true
			}
			if item.IsFinal {
				completed++
			}
			if onItem != nil {
				if callbackErr := onItem(item); callbackErr != nil {
					return callbackErr
				}
			}
			if completed >= n {
				return nil
			}
		case WSMessageTypeError:
			code := strings.TrimSpace(msg.ErrorCode)
			if code == "" {
				code = ErrorCodeWsClosed
			}
			return &ReverseError{Status: 502, Code: code, Message: msg.Error}
		case WSMessageTypeClose:
			return MapWSCloseCode(msg.CloseCode, msg.CloseReason)
		case WSMessageTypeTimeout:
			if sawMedium && completed == 0 {
				return &ReverseError{Status: 403, Code: ErrorCodeBlocked, Message: "blocked_no_final_image"}
			}
		}
	}
}

func isRetryableWSError(err *ReverseError) bool {
	if err == nil {
		return false
	}
	return err.Code == ErrorCodeWsClosed || err.Code == ErrorCodeConnectionFailed || err.Code == ErrorCodeRateLimitExceeded
}

var imageURLPattern = regexp.MustCompile(`/images/([a-zA-Z0-9-]+)\.(png|jpg|jpeg)`)

func classifyImage(url string, blob string, finalMinBytes int, mediumMinBytes int) (ImagineItem, bool) {
	if strings.TrimSpace(url) == "" || blob == "" {
		return ImagineItem{}, false
	}

	imageID := "unknown"
	ext := "jpg"
	match := imageURLPattern.FindStringSubmatch(url)
	if len(match) == 3 {
		imageID = strings.ToLower(match[1])
		ext = strings.ToLower(match[2])
	}

	blobSize := len(blob)
	isFinal := blobSize >= finalMinBytes
	stage := "preview"
	if isFinal {
		stage = "final"
	} else if blobSize > mediumMinBytes {
		stage = "medium"
	}

	return ImagineItem{
		Type:     "image",
		ImageID:  imageID,
		Ext:      ext,
		Stage:    stage,
		Blob:     blob,
		BlobSize: blobSize,
		URL:      url,
		IsFinal:  isFinal,
	}, true
}

func (m WSMessage) String() string {
	return fmt.Sprintf("type=%s close=%d err=%s", m.Type, m.CloseCode, m.ErrorCode)
}
