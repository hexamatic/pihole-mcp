package pihole

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLogin_KeepsFTLsReasonForRefusing(t *testing.T) {
	// FTL refuses a login with its session object, not its error envelope,
	// so the reason was lost and the error read "HTTP 401".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"session":{"valid":false,"totp":false,"sid":null,"validity":-1,"message":"password incorrect"},"took":0.1}`))
	}))
	defer srv.Close()

	err := New(srv.URL, "wrong", WithRetry(0, time.Second)).Get(context.Background(), "/info/version", nil)
	var authErr *AuthError
	if !errors.As(err, &authErr) || !strings.Contains(err.Error(), "authentication failed: password incorrect") {
		t.Errorf("err = %T %v, want an AuthError giving FTL's reason", err, err)
	}
}
