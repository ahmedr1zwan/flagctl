// Package api exposes the service's HTTP interface.
package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ahmedr1zwan/flagctl/internal/flags"
)

// FlagStore is the persistence surface needed by the implemented routes.
type FlagStore interface {
	Create(context.Context, string, flags.CreateInput) (flags.Flag, error)
	List(context.Context, string) ([]flags.Flag, error)
	Get(context.Context, string, string) (flags.Flag, error)
	Update(context.Context, string, string, flags.UpdateInput) (flags.Flag, error)
	Delete(context.Context, string, string) error
}

// NewHandler accepts the actual bound address, including the assigned port when
// port 0 is used. Only that Host (or localhost on the same port) is accepted.
func NewHandler(store FlagStore, address string) http.Handler {
	handler := &flagHandler{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", health)
	mux.HandleFunc("/v1/environments/{env}/flags", handler.collection)
	mux.HandleFunc("/v1/environments/{env}/flags/{key}", handler.item)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Route not found.")
	})
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusForbidden, "forbidden", "Cross-origin mutation requests are not allowed.")
	}))
	protected := protection.Handler(mux)
	_, port, _ := net.SplitHostPort(address)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		host := r.Host
		// HTTP clients omit the port in Host when using the default HTTP port.
		if port == "80" && (host == "localhost" || !strings.Contains(host, ":") || strings.HasSuffix(host, "]")) {
			host += ":80"
		}
		if !strings.EqualFold(host, address) && !strings.EqualFold(host, net.JoinHostPort("localhost", port)) {
			writeError(w, http.StatusForbidden, "forbidden", "Host must match the local service address.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		protected.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or HEAD for this endpoint.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	// A write failure means the client disconnected; there is no further response
	// to send. Never include request contents or credentials in error logs.
	_ = json.NewEncoder(w).Encode(struct {
		Status string `json:"status"`
	}{Status: "ok"})
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Error: errorDetail{Code: code, Message: message},
	})
}
