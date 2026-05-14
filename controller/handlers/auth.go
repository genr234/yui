package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"strings"
	"time"

	"kiosk/controller/internal/config"
)

const (
	pinAlgorithm       = "pbkdf2-sha256"
	pinIterations      = 210000
	pinKeyLength       = 32
	pinMinLength       = 6
	pinMaxLength       = 6
	pinMaxFailed       = 5
	pinBaseLockSeconds = 15
)

type AuthState struct {
	FailedAttempts int
	LockUntil      time.Time
}

type authStatus struct {
	Configured    bool  `json:"configured"`
	Locked        bool  `json:"locked"`
	RetryAfterSec int64 `json:"retry_after_seconds"`
	AttemptsLeft  int   `json:"attempts_left"`
}

type authResult struct {
	OK     bool       `json:"ok"`
	Token  string     `json:"token,omitempty"`
	Status authStatus `json:"status"`
}

func AuthCommands() []Command {
	return []Command{
		AuthStatusCommand{},
		AuthSetPINCommand{},
		AuthVerifyPINCommand{},
		AuthClearPINCommand{},
	}
}

type AuthStatusCommand struct{}

func (AuthStatusCommand) Name() string { return "auth.status" }

func (AuthStatusCommand) Handle(r *Registry, _ json.RawMessage) (any, error) {
	r.authMu.Lock()
	defer r.authMu.Unlock()
	return r.authStatusLocked(), nil
}

type AuthSetPINCommand struct{}

func (AuthSetPINCommand) Name() string { return "auth.setPin" }

func (AuthSetPINCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var req struct {
		PIN        string `json:"pin"`
		CurrentPIN string `json:"current_pin"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	if err := validatePIN(req.PIN); err != nil {
		return nil, err
	}

	r.authMu.Lock()
	defer r.authMu.Unlock()

	if pinConfigured(r.cfg) {
		if err := r.verifyPINLocked(req.CurrentPIN); err != nil {
			return r.authStatusLocked(), err
		}
	}

	pinHash, err := hashPIN(req.PIN)
	if err != nil {
		return nil, err
	}
	cfg := r.cfg
	cfg.AdminPIN = pinHash
	if err := config.Save(cfg); err != nil {
		return nil, err
	}
	r.cfg = cfg
	r.authState = AuthState{}
	token, err := r.rotateAuthTokenLocked()
	if err != nil {
		return nil, err
	}
	return authResult{OK: true, Token: token, Status: r.authStatusLocked()}, nil
}

type AuthVerifyPINCommand struct{}

func (AuthVerifyPINCommand) Name() string { return "auth.verifyPin" }

func (AuthVerifyPINCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var req struct {
		PIN string `json:"pin"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	r.authMu.Lock()
	defer r.authMu.Unlock()

	if !pinConfigured(r.cfg) {
		return authResult{OK: true, Status: r.authStatusLocked()}, nil
	}
	if err := r.verifyPINLocked(req.PIN); err != nil {
		return authResult{OK: false, Status: r.authStatusLocked()}, err
	}
	token, err := r.rotateAuthTokenLocked()
	if err != nil {
		return nil, err
	}
	return authResult{OK: true, Token: token, Status: r.authStatusLocked()}, nil
}

type AuthClearPINCommand struct{}

func (AuthClearPINCommand) Name() string { return "auth.clearPin" }

func (AuthClearPINCommand) Handle(r *Registry, params json.RawMessage) (any, error) {
	var req struct {
		CurrentPIN string `json:"current_pin"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	r.authMu.Lock()
	defer r.authMu.Unlock()

	if pinConfigured(r.cfg) {
		if err := r.verifyPINLocked(req.CurrentPIN); err != nil {
			return r.authStatusLocked(), err
		}
	}

	cfg := r.cfg
	cfg.AdminPIN = config.PINHash{}
	if err := config.Save(cfg); err != nil {
		return nil, err
	}
	r.cfg = cfg
	r.authState = AuthState{}
	r.authToken = ""
	return r.authStatusLocked(), nil
}

func (r *Registry) Authorize(method string, params json.RawMessage) error {
	if publicAuthMethod(method) {
		return nil
	}

	r.authMu.Lock()
	defer r.authMu.Unlock()

	if !pinConfigured(r.cfg) {
		return nil
	}

	var req struct {
		Token string `json:"_auth_token"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &req)
	}
	if r.authToken == "" || req.Token == "" || subtle.ConstantTimeCompare([]byte(req.Token), []byte(r.authToken)) != 1 {
		return fmt.Errorf("admin PIN required")
	}
	return nil
}

func publicAuthMethod(method string) bool {
	switch method {
	case "auth.status", "auth.setPin", "auth.verifyPin", "status.get", "diagnostics.get", "config.get", "accounts.status":
		return true
	default:
		return false
	}
}

func (r *Registry) authStatusLocked() authStatus {
	now := time.Now()
	retryAfter := int64(0)
	if r.authState.LockUntil.After(now) {
		retryAfter = int64(time.Until(r.authState.LockUntil).Seconds()) + 1
	} else if !r.authState.LockUntil.IsZero() {
		r.authState.LockUntil = time.Time{}
	}
	attemptsLeft := pinMaxFailed - r.authState.FailedAttempts
	if attemptsLeft < 0 {
		attemptsLeft = 0
	}
	return authStatus{
		Configured:    pinConfigured(r.cfg),
		Locked:        retryAfter > 0,
		RetryAfterSec: retryAfter,
		AttemptsLeft:  attemptsLeft,
	}
}

func (r *Registry) verifyPINLocked(pin string) error {
	status := r.authStatusLocked()
	if status.Locked {
		return fmt.Errorf("too many attempts; try again in %d seconds", status.RetryAfterSec)
	}
	if err := validatePIN(pin); err != nil {
		r.recordFailedPINLocked()
		return fmt.Errorf("invalid PIN")
	}
	ok, err := verifyPIN(pin, r.cfg.AdminPIN)
	if err != nil {
		return err
	}
	if !ok {
		r.recordFailedPINLocked()
		return fmt.Errorf("invalid PIN")
	}
	r.authState = AuthState{}
	return nil
}

func (r *Registry) recordFailedPINLocked() {
	r.authState.FailedAttempts++
	if r.authState.FailedAttempts < pinMaxFailed {
		return
	}
	overage := r.authState.FailedAttempts - pinMaxFailed
	lockSeconds := pinBaseLockSeconds << min(overage, 4)
	r.authState.LockUntil = time.Now().Add(time.Duration(lockSeconds) * time.Second)
}

func (r *Registry) rotateAuthTokenLocked() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawStdEncoding.EncodeToString(raw)
	r.authToken = token
	return token, nil
}

func pinConfigured(cfg config.Config) bool {
	return cfg.AdminPIN.Algorithm != "" && cfg.AdminPIN.Salt != "" && cfg.AdminPIN.Hash != ""
}

func validatePIN(pin string) error {
	if len(pin) < pinMinLength || len(pin) > pinMaxLength {
		return fmt.Errorf("PIN must be %d-%d digits", pinMinLength, pinMaxLength)
	}
	for _, ch := range pin {
		if ch < '0' || ch > '9' {
			return fmt.Errorf("PIN must contain only digits")
		}
	}
	if strings.Count(pin, string(pin[0])) == len(pin) {
		return fmt.Errorf("PIN cannot repeat the same digit")
	}
	return nil
}

func hashPIN(pin string) (config.PINHash, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return config.PINHash{}, err
	}
	hash := pbkdf2Key([]byte(pin), salt, pinIterations, pinKeyLength, sha256.New)
	return config.PINHash{
		Algorithm:  pinAlgorithm,
		Salt:       base64.RawStdEncoding.EncodeToString(salt),
		Hash:       base64.RawStdEncoding.EncodeToString(hash),
		Iterations: pinIterations,
		KeyLength:  pinKeyLength,
	}, nil
}

func verifyPIN(pin string, stored config.PINHash) (bool, error) {
	if stored.Algorithm != pinAlgorithm {
		return false, fmt.Errorf("unsupported PIN hash algorithm")
	}
	salt, err := base64.RawStdEncoding.DecodeString(stored.Salt)
	if err != nil {
		return false, fmt.Errorf("decode PIN salt: %w", err)
	}
	expected, err := base64.RawStdEncoding.DecodeString(stored.Hash)
	if err != nil {
		return false, fmt.Errorf("decode PIN hash: %w", err)
	}
	iterations := stored.Iterations
	if iterations <= 0 {
		iterations = pinIterations
	}
	keyLength := stored.KeyLength
	if keyLength <= 0 {
		keyLength = len(expected)
	}
	actual := pbkdf2Key([]byte(pin), salt, iterations, keyLength, sha256.New)
	if len(actual) != len(expected) {
		return false, nil
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func pbkdf2Key(password, salt []byte, iter, keyLen int, h func() hash.Hash) []byte {
	prf := hmac.New(h, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen
	var buf [4]byte
	dk := make([]byte, 0, numBlocks*hashLen)
	u := make([]byte, hashLen)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		binary.BigEndian.PutUint32(buf[:], uint32(block))
		prf.Write(buf[:])
		sum := prf.Sum(u[:0])
		t := append([]byte(nil), sum...)
		for i := 1; i < iter; i++ {
			prf.Reset()
			prf.Write(sum)
			sum = prf.Sum(u[:0])
			for x := range t {
				t[x] ^= sum[x]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}
