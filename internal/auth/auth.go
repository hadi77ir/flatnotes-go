package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hadi77ir/flatnotes-go/internal/config"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type Service interface {
	Login(LoginRequest) (TokenResponse, error)
	Authenticate(*http.Request) error
}

type NoopService struct{}

func (NoopService) Login(LoginRequest) (TokenResponse, error) {
	return TokenResponse{}, ErrInvalidCredentials
}

func (NoopService) Authenticate(*http.Request) error {
	return nil
}

type LocalService struct {
	username          string
	password          string
	secretKey         []byte
	sessionExpiryDays int
	totpSecret        []byte
	lastUsedTOTP      string
	mu                sync.Mutex
}

func NewLocalService(cfg config.Config) *LocalService {
	return &LocalService{
		username:          strings.ToLower(cfg.Username),
		password:          cfg.Password,
		secretKey:         []byte(cfg.SecretKey),
		sessionExpiryDays: cfg.SessionExpiryDays,
		totpSecret:        []byte(cfg.TOTPKey),
	}
}

func (s *LocalService) Login(data LoginRequest) (TokenResponse, error) {
	usernameOK := subtle.ConstantTimeCompare([]byte(s.username), []byte(strings.ToLower(data.Username))) == 1
	expectedPassword := s.password
	var currentTOTP string
	if len(s.totpSecret) > 0 {
		currentTOTP = totp(s.totpSecret, time.Now().UTC(), 30, 6)
		expectedPassword += currentTOTP
	}
	passwordOK := subtle.ConstantTimeCompare([]byte(expectedPassword), []byte(data.Password)) == 1
	s.mu.Lock()
	defer s.mu.Unlock()
	if !usernameOK || !passwordOK || (currentTOTP != "" && currentTOTP == s.lastUsedTOTP) {
		return TokenResponse{}, ErrInvalidCredentials
	}
	if currentTOTP != "" {
		s.lastUsedTOTP = currentTOTP
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": s.username,
		"exp": time.Now().UTC().Add(time.Duration(s.sessionExpiryDays) * 24 * time.Hour).Unix(),
		"iat": time.Now().UTC().Unix(),
	})
	signed, err := token.SignedString(s.secretKey)
	if err != nil {
		return TokenResponse{}, err
	}
	return TokenResponse{AccessToken: signed, TokenType: "bearer"}, nil
}

func (s *LocalService) Authenticate(r *http.Request) error {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		if cookie, err := r.Cookie("token"); err == nil {
			token = cookie.Value
		}
	}
	if token == "" {
		return ErrInvalidCredentials
	}
	claims := jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(token, &claims, func(token *jwt.Token) (interface{}, error) {
		return s.secretKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !parsed.Valid {
		return ErrInvalidCredentials
	}
	if strings.ToLower(claims.Subject) != s.username {
		return ErrInvalidCredentials
	}
	return nil
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		return parts[1]
	}
	return ""
}

func totp(secret []byte, at time.Time, period, digits int) string {
	key := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(key)
	if err != nil {
		decoded = secret
	}
	counter := uint64(at.Unix() / int64(period))
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, decoded)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (int(sum[offset])&0x7f)<<24 |
		(int(sum[offset+1])&0xff)<<16 |
		(int(sum[offset+2])&0xff)<<8 |
		(int(sum[offset+3]) & 0xff)
	mod := int(math.Pow10(digits))
	return fmt.Sprintf("%0*d", digits, code%mod)
}
