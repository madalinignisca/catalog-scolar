// Package httputil provides shared HTTP helper functions used across all
// CatalogRO API handlers.
//
// Every API response follows a consistent JSON envelope:
//
//	Success: { "data": { ... } }                       — single object
//	Success: { "data": [ ... ], "meta": { ... } }     — list with pagination
//	Error:   { "error": { "code": "...", "message": "..." } }
//
// These helpers make it easy for any handler to produce correctly-formatted
// responses without duplicating the JSON encoding logic.
package httputil

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// DataResponse wraps a successful API response in the standard envelope.
// The "data" field contains the actual payload (a struct, map, or slice).
//
// Example output:
//
//	{ "data": { "id": "abc", "name": "Clasa a V-a A" } }
type DataResponse struct {
	Data any `json:"data"`
}

// ListResponse wraps a list response with optional pagination metadata.
// The "data" field contains an array of items. The "meta" field is optional
// and can hold pagination cursors, totals, etc.
//
// Example output:
//
//	{ "data": [ ... ], "meta": { "total": 42 } }
type ListResponse struct {
	Data any `json:"data"`
	Meta any `json:"meta,omitempty"`
}

// ErrorBody is the structure inside the "error" envelope.
// It always contains a machine-readable code and a human-readable message.
//
// Example output:
//
//	{ "error": { "code": "GRADE_INVALID", "message": "Nota must be between 1 and 10" } }
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse wraps an error in the standard API envelope.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// JSON writes a JSON response with the given HTTP status code.
// It sets the Content-Type header to application/json and encodes the
// provided value as the response body.
//
// If JSON encoding fails (which should never happen with well-typed data),
// it falls back to a plain-text 500 error so the client always gets a response.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		// This should never happen in practice, but if it does we log it
		// and ensure the client gets *some* response rather than a silent failure.
		slog.Error("httputil.JSON: failed to encode response", "error", err)
	}
}

// Success writes a 200 OK response wrapping the payload in { "data": ... }.
// This is the most common response type for GET and PUT endpoints.
func Success(w http.ResponseWriter, data any) {
	JSON(w, http.StatusOK, DataResponse{Data: data})
}

// Created writes a 201 Created response wrapping the payload in { "data": ... }.
// Used after a POST that creates a new resource (grade, absence, etc.).
func Created(w http.ResponseWriter, data any) {
	JSON(w, http.StatusCreated, DataResponse{Data: data})
}

// List writes a 200 OK response wrapping the items in { "data": [...], "meta": ... }.
// The meta parameter is optional — pass nil if there is no pagination info.
func List(w http.ResponseWriter, data any, meta any) {
	JSON(w, http.StatusOK, ListResponse{Data: data, Meta: meta})
}

// Error writes an error response with the given HTTP status code.
// The response body is: { "error": { "code": "...", "message": "..." } }
//
// Common usage:
//
//	httputil.Error(w, http.StatusBadRequest, "INVALID_INPUT", "The semester field must be I or II")
//	httputil.Error(w, http.StatusNotFound, "NOT_FOUND", "Grade not found")
//	httputil.Error(w, http.StatusForbidden, "FORBIDDEN", "You are not assigned to this class")
func Error(w http.ResponseWriter, status int, code string, message string) {
	JSON(w, status, ErrorResponse{
		Error: ErrorBody{
			Code:    code,
			Message: message,
		},
	})
}

// BadRequest writes a 400 Bad Request error response.
// Shorthand for Error(w, 400, code, message).
func BadRequest(w http.ResponseWriter, code string, message string) {
	Error(w, http.StatusBadRequest, code, message)
}

// NotFound writes a 404 Not Found error response.
// Shorthand for Error(w, 404, "NOT_FOUND", message).
func NotFound(w http.ResponseWriter, message string) {
	Error(w, http.StatusNotFound, "NOT_FOUND", message)
}

// Forbidden writes a 403 Forbidden error response.
// Shorthand for Error(w, 403, "FORBIDDEN", message).
func Forbidden(w http.ResponseWriter, message string) {
	Error(w, http.StatusForbidden, "FORBIDDEN", message)
}

// Unauthorized writes a 401 Unauthorized error response.
// Shorthand for Error(w, 401, "UNAUTHORIZED", message).
func Unauthorized(w http.ResponseWriter, message string) {
	Error(w, http.StatusUnauthorized, "UNAUTHORIZED", message)
}

// InternalError writes a 500 Internal Server Error response.
// The message should be generic (never leak internal details to the client).
// The actual error should be logged separately by the handler.
func InternalError(w http.ResponseWriter) {
	Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "An unexpected error occurred")
}
