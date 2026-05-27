package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hadi77ir/flatnotes-go/internal/config"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func testService() *LocalService {
	return NewLocalService(config.Config{
		Username:          "user",
		Password:          "pass",
		SecretKey:         testSecret,
		SessionExpiryDays: 1,
	})
}

func TestLocalServiceLoginAndAuthenticateBearerAndCookie(t *testing.T) {
	svc := testService()
	token, err := svc.Login(LoginRequest{Username: "USER", Password: "pass"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if token.TokenType != "bearer" || token.AccessToken == "" {
		t.Fatalf("unexpected token response: %+v", token)
	}
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	if err := svc.Authenticate(req); err != nil {
		t.Fatalf("Authenticate(bearer) error = %v", err)
	}
	req, _ = http.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: token.AccessToken})
	if err := svc.Authenticate(req); err != nil {
		t.Fatalf("Authenticate(cookie) error = %v", err)
	}
}

func TestLocalServiceRejectsInvalidCredentialsAndTokens(t *testing.T) {
	svc := testService()
	if _, err := svc.Login(LoginRequest{Username: "user", Password: "wrong"}); err == nil {
		t.Fatal("Login() accepted wrong password")
	}
	if _, err := svc.Login(LoginRequest{Username: "other", Password: "pass"}); err == nil {
		t.Fatal("Login() accepted wrong username")
	}
	if err := svc.Authenticate(mustRequestWithBearer("bad")); err == nil {
		t.Fatal("Authenticate() accepted malformed token")
	}
	expired := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})
	expiredString, err := expired.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Authenticate(mustRequestWithBearer(expiredString)); err == nil {
		t.Fatal("Authenticate() accepted expired token")
	}
	wrongSubject := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "other",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	wrongSubjectString, err := wrongSubject.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Authenticate(mustRequestWithBearer(wrongSubjectString)); err == nil {
		t.Fatal("Authenticate() accepted wrong subject")
	}
	wrongAlg := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.MapClaims{
		"sub": "user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	wrongAlgString, err := wrongAlg.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Authenticate(mustRequestWithBearer(wrongAlgString)); err == nil {
		t.Fatal("Authenticate() accepted non-HS256 token")
	}
}

func TestNoopService(t *testing.T) {
	if err := (NoopService{}).Authenticate(mustRequestWithBearer("")); err != nil {
		t.Fatalf("Noop Authenticate() error = %v", err)
	}
	if _, err := (NoopService{}).Login(LoginRequest{}); err == nil {
		t.Fatal("Noop Login() succeeded")
	}
}

func TestTOTPReuseRejected(t *testing.T) {
	svc := NewLocalService(config.Config{
		Username:          "user",
		Password:          "pass",
		SecretKey:         testSecret,
		SessionExpiryDays: 1,
		TOTPKey:           "seed",
	})
	code := totp([]byte("seed"), time.Now().UTC(), 30, 6)
	if _, err := svc.Login(LoginRequest{Username: "user", Password: "pass" + code}); err != nil {
		t.Fatalf("Login() with TOTP error = %v", err)
	}
	if _, err := svc.Login(LoginRequest{Username: "user", Password: "pass" + code}); err == nil {
		t.Fatal("Login() allowed TOTP reuse")
	}
}

func mustRequestWithBearer(token string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}
