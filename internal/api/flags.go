package api

import (
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/ahmedr1zwan/flagctl/internal/flags"
	"github.com/ahmedr1zwan/flagctl/internal/store"
)

type flagHandler struct {
	store FlagStore
}

// Keep the API's derived identity in the response, without making it a stored
// field or an input that clients can override.
type flagResponse struct {
	flags.Flag
	ID string `json:"id"`
}

func newFlagResponse(flag flags.Flag) flagResponse {
	return flagResponse{Flag: flag, ID: flag.Environment + "/" + flag.Key}
}

func (h *flagHandler) collection(w http.ResponseWriter, r *http.Request) {
	environment := r.PathValue("env")
	if err := flags.ValidateEnvironment(environment); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		items, err := h.store.List(r.Context(), environment)
		if err != nil {
			storageError(w, "list", err)
			return
		}
		response := make([]flagResponse, len(items))
		for index, flag := range items {
			response[index] = newFlagResponse(flag)
		}
		writeJSON(w, http.StatusOK, struct {
			Flags []flagResponse `json:"flags"`
		}{Flags: response})
	case http.MethodPost:
		h.create(w, r, environment)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET, HEAD, or POST for this endpoint.")
	}
}

func (h *flagHandler) item(w http.ResponseWriter, r *http.Request) {
	environment, key := r.PathValue("env"), r.PathValue("key")
	if err := flags.ValidateIdentity(environment, key); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		flag, err := h.store.Get(r.Context(), environment, key)
		if err != nil {
			storageError(w, "get", err)
			return
		}
		writeJSON(w, http.StatusOK, newFlagResponse(flag))
	case http.MethodPatch:
		h.update(w, r, environment, key)
	case http.MethodDelete:
		if err := h.store.Delete(r.Context(), environment, key); err != nil {
			storageError(w, "delete", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, HEAD, PATCH, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET, HEAD, PATCH, or DELETE for this endpoint.")
	}
}

func (h *flagHandler) create(w http.ResponseWriter, r *http.Request, environment string) {
	if !requireJSON(w, r) {
		return
	}
	input, err := decodeCreate(w, r)
	if err != nil {
		writeDecodeError(w, err)
		return
	}
	if err := input.Validate(environment); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	flag, err := h.store.Create(r.Context(), environment, input)
	if err != nil {
		storageError(w, "create", err)
		return
	}
	w.Header().Set("Location", "/v1/environments/"+flag.Environment+"/flags/"+flag.Key)
	writeJSON(w, http.StatusCreated, newFlagResponse(flag))
}

func (h *flagHandler) update(w http.ResponseWriter, r *http.Request, environment, key string) {
	if !requireJSON(w, r) {
		return
	}
	input, err := decodeUpdate(w, r)
	if err != nil {
		writeDecodeError(w, err)
		return
	}
	if err := input.Validate(environment, key); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	flag, err := h.store.Update(r.Context(), environment, key, input)
	if err != nil {
		storageError(w, "update", err)
		return
	}
	writeJSON(w, http.StatusOK, newFlagResponse(flag))
}

func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	encoding := r.Header.Get("Content-Encoding")
	if err != nil || mediaType != "application/json" ||
		(params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) ||
		(encoding != "" && !strings.EqualFold(encoding, "identity")) {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use uncompressed application/json with UTF-8 encoding.")
		return false
	}
	return true
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Request body must not exceed 16384 bytes.")
	} else {
		writeError(w, http.StatusBadRequest, "invalid_request", "Body must be one JSON object with the operation's required fields and valid types; nulls, duplicate fields, and unknown fields are not allowed.")
	}
}

func storageError(w http.ResponseWriter, operation string, err error) {
	switch {
	case errors.Is(err, store.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "already_exists", "A flag with this key already exists in this environment.")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Flag not found.")
	default:
		// Driver errors may contain data or filesystem details. Log only the
		// operation, never the raw error or request content.
		slog.Error("flag storage operation failed", "operation", operation)
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not complete the flag operation.")
	}
}
