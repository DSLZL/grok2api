package errors

import (
	"encoding/json"
	stdErrors "errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const (
	ErrorTypeInvalidRequest     = "invalid_request_error"
	ErrorTypeAuthentication     = "authentication_error"
	ErrorTypePermission         = "permission_error"
	ErrorTypeNotFound           = "not_found_error"
	ErrorTypeRateLimit          = "rate_limit_error"
	ErrorTypeServer             = "server_error"
	ErrorTypeServiceUnavailable = "service_unavailable_error"

	InvalidJSONMessage = "Invalid JSON in request body. Please check for trailing commas or syntax errors."
)

type ErrorEnvelope struct {
	Error ErrorObject `json:"error"`
}

type ErrorObject struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   any    `json:"param"`
	Code    any    `json:"code"`
}

type AppError struct {
	Message    string
	ErrorType  string
	Code       any
	Param      any
	StatusCode int
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func NewAppError(message string, errorType string, code any, param any, statusCode int) *AppError {
	if errorType == "" {
		errorType = ErrorTypeServer
	}
	if statusCode <= 0 {
		statusCode = http.StatusInternalServerError
	}
	return &AppError{
		Message:    message,
		ErrorType:  errorType,
		Code:       code,
		Param:      param,
		StatusCode: statusCode,
	}
}

func ErrorResponse(message string, errorType string, param any, code any) ErrorEnvelope {
	if errorType == "" {
		errorType = ErrorTypeInvalidRequest
	}
	return ErrorEnvelope{
		Error: ErrorObject{
			Message: message,
			Type:    errorType,
			Param:   param,
			Code:    code,
		},
	}
}

func HTTPStatusToErrorTypeMapping(statusCode int) (string, any) {
	switch statusCode {
	case http.StatusUnauthorized:
		return ErrorTypeAuthentication, "invalid_api_key"
	case http.StatusForbidden:
		return ErrorTypePermission, "insufficient_quota"
	case http.StatusNotFound:
		return ErrorTypeNotFound, "model_not_found"
	case http.StatusTooManyRequests:
		return ErrorTypeRateLimit, "rate_limit_exceeded"
	case http.StatusBadRequest:
		return ErrorTypeInvalidRequest, nil
	default:
		return ErrorTypeServer, nil
	}
}

func ValidationErrorResponse(message string, code string, loc []any) ErrorEnvelope {
	finalMessage := message
	finalParam := extractValidationParam(loc)

	if code == "json_invalid" || strings.Contains(message, "JSON") {
		finalMessage = InvalidJSONMessage
		finalParam = "body"
	}

	if code == "" {
		code = "invalid_value"
	}

	return ErrorResponse(finalMessage, ErrorTypeInvalidRequest, finalParam, code)
}

func extractValidationParam(loc []any) any {
	if len(loc) == 0 {
		return nil
	}

	parts := make([]string, 0, len(loc))
	for _, item := range loc {
		switch v := item.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			continue
		case float32, float64:
			if isNumericString(fmt.Sprintf("%v", v)) {
				continue
			}
		case string:
			if isNumericString(v) {
				continue
			}
			if strings.TrimSpace(v) != "" {
				parts = append(parts, v)
			}
		default:
			text := strings.TrimSpace(fmt.Sprintf("%v", v))
			if text == "" || isNumericString(text) {
				continue
			}
			parts = append(parts, text)
		}
	}

	if len(parts) == 0 {
		return nil
	}

	return strings.Join(parts, ".")
}

func isNumericString(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	_, err := strconv.Atoi(trimmed)
	return err == nil
}

func WriteErrorEnvelope(w http.ResponseWriter, statusCode int, envelope ErrorEnvelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(envelope)
}

func WriteHTTPError(w http.ResponseWriter, statusCode int, detail string) {
	errorType, code := HTTPStatusToErrorTypeMapping(statusCode)
	WriteErrorEnvelope(w, statusCode, ErrorResponse(detail, errorType, nil, code))
}

func SSEErrorFrame(err error) string {
	payload := envelopeFromError(err)
	body, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		fallback := ErrorResponse("stream_error", ErrorTypeServer, nil, "stream_error")
		body, _ = json.Marshal(fallback)
	}
	return "event: error\ndata: " + string(body) + "\n\ndata: [DONE]\n\n"
}

func envelopeFromError(err error) ErrorEnvelope {
	var appErr *AppError
	if stdErrors.As(err, &appErr) && appErr != nil {
		return ErrorResponse(appErr.Message, appErr.ErrorType, appErr.Param, appErr.Code)
	}

	message := "stream_error"
	if err != nil {
		if trimmed := strings.TrimSpace(err.Error()); trimmed != "" {
			message = trimmed
		}
	}

	return ErrorResponse(message, ErrorTypeServer, nil, "stream_error")
}
