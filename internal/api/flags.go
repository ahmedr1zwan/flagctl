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
		writeJSON(w, http.StatusOK, struct {
			Flags []flags.Flag `json:"flags"`
		}{Flags: items})
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
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or HEAD for this endpoint.")
		return
	}
	flag, err := h.store.Get(r.Context(), environment, key)
	if err != nil {
		storageError(w, "get", err)
		return
	}
	writeJSON(w, http.StatusOK, flag)
}

func (h *flagHandler) create(w http.ResponseWriter, r *http.Request, environment string) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	encoding := r.Header.Get("Content-Encoding")
	if err != nil || mediaType != "application/json" ||
		(params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) ||
		(encoding != "" && !strings.EqualFold(encoding, "identity")) {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use uncompressed application/json with UTF-8 encoding.")
		return
	}
	input, err := decodeCreate(w, r)
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "Request body must not exceed 16384 bytes.")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_request", "Body must be one JSON object containing key and optional description/enabled, with valid types, no nulls, and no duplicate or unknown fields.")
		}
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
	writeJSON(w, http.StatusCreated, flag)
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
