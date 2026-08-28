package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "healthy", status: http.StatusOK},
		{name: "server error", status: http.StatusInternalServerError, wantErr: true},
		{name: "not found", status: http.StatusNotFound, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			err := healthcheck(srv.URL + "/healthz")
			if (err != nil) != tt.wantErr {
				t.Errorf("healthcheck error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHealthcheckUnreachable(t *testing.T) {
	// A port nothing is listening on must fail, not hang.
	if err := healthcheck("http://127.0.0.1:1/healthz"); err == nil {
		t.Error("healthcheck on an unreachable server returned nil, want an error")
	}
}

func TestEnv(t *testing.T) {
	tests := []struct {
		name     string
		set      bool
		value    string
		fallback string
		want     string
	}{
		{name: "unset uses fallback", fallback: "8080", want: "8080"},
		{name: "set wins", set: true, value: "9000", fallback: "8080", want: "9000"},
		{name: "blank uses fallback", set: true, value: "", fallback: "8080", want: "8080"},
		{name: "whitespace uses fallback", set: true, value: "   ", fallback: "8080", want: "8080"},
		{name: "value is trimmed", set: true, value: "  9000 ", fallback: "8080", want: "9000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const key = "BUYFORBUB_TEST_VAR"
			if tt.set {
				t.Setenv(key, tt.value)
			}
			if got := env(key, tt.fallback); got != tt.want {
				t.Errorf("env(%q, %q) = %q, want %q", key, tt.fallback, got, tt.want)
			}
		})
	}
}
