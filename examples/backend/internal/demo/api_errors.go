package demo

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// APIError is the stable house-contract representation. Provider diagnostics
// must be placed in Extra; never expose raw internal error text here.
type APIError struct {
	Status    int            `json:"-"`
	Message   string         `json:"message"`
	ErrorCode string         `json:"error_code"`
	Extra     map[string]any `json:"extra,omitempty"`
}

func malformedRequest() *APIError {
	return &APIError{Status: http.StatusBadRequest, Message: "Requisição malformada", ErrorCode: "MALFORMED_REQUEST"}
}

func payloadTooLarge() *APIError {
	return &APIError{Status: http.StatusRequestEntityTooLarge, Message: "O corpo da requisição excede o limite permitido", ErrorCode: "PAYLOAD_TOO_LARGE"}
}

func invalidParams(field, message string) *APIError {
	if field == "" {
		field = "body"
	}
	return &APIError{
		Status: http.StatusUnprocessableEntity, Message: "Parâmetros inválidos", ErrorCode: "INVALID_PARAMS",
		Extra: map[string]any{"validation_errors": []map[string]string{{"field": field, "message": message}}},
	}
}

func unauthorized() *APIError {
	return &APIError{Status: http.StatusUnauthorized, Message: "Autenticação necessária ou inválida", ErrorCode: "UNAUTHORIZED"}
}

func resourceNotFound(kind string) *APIError {
	code, message := "RESOURCE_NOT_FOUND", "Recurso não encontrado"
	switch kind {
	case "order":
		code, message = "ORDER_NOT_FOUND", "Pedido não encontrado"
	case "checkout":
		code, message = "CHECKOUT_NOT_FOUND", "Checkout não encontrado"
	}
	return &APIError{Status: http.StatusNotFound, Message: message, ErrorCode: code}
}

func unexpectedError() *APIError {
	return &APIError{Status: http.StatusInternalServerError, Message: "Ocorreu um erro inesperado", ErrorCode: "UNEXPECTED_ERROR"}
}

func apiConflict(code, message string) *APIError {
	return &APIError{Status: http.StatusConflict, Message: message, ErrorCode: code}
}

func dependencyError(code, message, providerCode string) *APIError {
	extra := map[string]any{}
	if providerCode != "" {
		extra["provider_code"] = providerCode
	}
	return &APIError{Status: http.StatusBadGateway, Message: message, ErrorCode: code, Extra: extra}
}

func writeAPIError(w http.ResponseWriter, apiErr *APIError) {
	if apiErr == nil {
		apiErr = unexpectedError()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(apiErr.Status)
	_ = json.NewEncoder(w).Encode(apiErr)
}

func decodeAPIError(err error) *APIError {
	if err == nil {
		return nil
	}
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return payloadTooLarge()
	}
	var syntaxError *json.SyntaxError
	if errors.As(err, &syntaxError) || errors.Is(err, errMalformedJSON) {
		return malformedRequest()
	}
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		return invalidParams(typeError.Field, "tipo de dado inválido")
	}
	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		field := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), "\"")
		return invalidParams(field, "campo não permitido")
	}
	var validation *requestValidationError
	if errors.As(err, &validation) {
		return invalidParams(validation.field, validation.message)
	}
	return malformedRequest()
}

var errMalformedJSON = errors.New("malformed JSON")

type requestValidationError struct {
	field   string
	message string
}

func (e *requestValidationError) Error() string { return fmt.Sprintf("%s: %s", e.field, e.message) }
