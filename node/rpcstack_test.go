package node

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVirtualHostHandler_CaseSensitivity(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := newVHostHandler([]string{"localhost"}, okHandler)

	tests := []struct {
		name       string
		hostHeader string
		wantStatus int
	}{
		{"lowercase", "localhost", http.StatusOK},
		{"uppercase", "LOCALHOST", http.StatusOK},
		{"mixed_case", "LocalHost", http.StatusOK},
		{"lowercase_with_port", "localhost:35997", http.StatusOK},
		{"mixed_case_with_port", "LocalHost:35997", http.StatusOK},
		{"unrelated_host", "evil.example.com", http.StatusForbidden},
		{"unrelated_host_with_port", "evil.example.com:35997", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://"+tt.hostHeader+"/", nil)
			req.Host = tt.hostHeader
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("Host %q: got status %d, want %d", tt.hostHeader, rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestVirtualHostHandler_IPLiteral(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := newVHostHandler([]string{"localhost"}, okHandler)

	// IP literals should always be served regardless of vhost config
	for _, host := range []string{"127.0.0.1", "127.0.0.1:35997", "[::1]:35997"} {
		req := httptest.NewRequest(http.MethodPost, "http://"+host+"/", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("Host %q: got status %d, want %d", host, rec.Code, http.StatusOK)
		}
	}
}

func TestVirtualHostHandler_Wildcard(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := newVHostHandler([]string{"*"}, okHandler)

	req := httptest.NewRequest(http.MethodPost, "http://anything.example.com/", nil)
	req.Host = "anything.example.com"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("wildcard: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestVirtualHostHandler_EmptyHost(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := newVHostHandler([]string{"localhost"}, okHandler)

	// Empty Host header should be served (browser would set it)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/", nil)
	req.Host = ""
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("empty host: got status %d, want %d", rec.Code, http.StatusOK)
	}
}
