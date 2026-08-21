package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Card token lifetimes.
const (
	// cardDataTokenTTL is how long a freshly minted token stays usable. Short,
	// because the token is handed to a browser and is the only credential the
	// data endpoint requires.
	cardDataTokenTTL = 5 * time.Minute

	// cardDataTokenMaxTTL bounds the validity we will honour on an incoming
	// token, whatever its exp claims. The data endpoint is not HMAC
	// authenticated, so without a ceiling a long-lived token would turn it
	// into a standing encryption oracle.
	cardDataTokenMaxTTL = 10 * time.Minute
)

// Token types accepted by POST /cards/v1/token/{tokenType}.
const (
	cardTokenTypeCardData  = "card-data"
	cardTokenTypePin       = "pin"
	cardTokenTypePinChange = "pin-change"
)

// Paths the browser is pointed at to exchange a token for sensitive data.
const (
	cardDataPath = "/cards/v1/token/card-data/data"
	cardPinPath  = "/cards/v1/token/pin/data"
)

// CardTokenClaims is the payload of a card token.
type CardTokenClaims struct {
	// TokenType records which endpoint the token is for, so a token minted to
	// read a PIN cannot be replayed to change one.
	TokenType string `json:"tokenType"`
	CardID    string `json:"cardId"`
	// PublicKey is the caller's RSA public key, base64-encoded SPKI. The data
	// endpoints encrypt their response with it so the plaintext is readable
	// only by whoever holds the matching private key.
	PublicKey string `json:"publicKey,omitempty"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// generateCardToken builds an HS256 JWT for the supplied claims.
func generateCardToken(secret string, claims CardTokenClaims) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// parseCardToken verifies the signature and expiry of a card token and returns
// its claims. A token with no exp, an expired exp, or an exp further out than
// cardDataTokenMaxTTL is rejected.
func parseCardToken(secret, token string) (*CardTokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format")
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	// Constant-time compare: this runs on an unauthenticated endpoint.
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, errors.New("invalid token signature")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid token payload: %w", err)
	}

	var claims CardTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("invalid token claims: %w", err)
	}

	// A missing or zero exp would mean a token that never expires.
	if claims.ExpiresAt <= 0 {
		return nil, errors.New("token missing exp claim")
	}
	now := time.Now().Unix()
	if now > claims.ExpiresAt {
		return nil, errors.New("token expired")
	}
	if claims.ExpiresAt-now > int64(cardDataTokenMaxTTL.Seconds()) {
		return nil, errors.New("token exceeds maximum allowed validity")
	}

	return &claims, nil
}

// encryptWithBase64SPKI encrypts plaintext with a base64-encoded SPKI RSA
// public key — the format produced by exporting a WebCrypto key as 'spki' and
// base64-encoding it, and the format node-rsa reads back.
//
// PKCS#1 v1.5 padding is used deliberately: consumers decrypt with
// encryptionScheme 'pkcs1', so OAEP would fail to decrypt.
func encryptWithBase64SPKI(publicKeyB64 string, plaintext []byte) (string, error) {
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyB64))
	if err != nil {
		return "", fmt.Errorf("public key is not valid base64: %w", err)
	}

	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return "", fmt.Errorf("public key is not a valid SPKI key: %w", err)
	}

	rsaPub, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("public key is not RSA")
	}

	cipherText, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, plaintext)
	if err != nil {
		return "", fmt.Errorf("encryption failed: %w", err)
	}

	return base64.StdEncoding.EncodeToString(cipherText), nil
}

// serviceKeyPair is the RSA key pair this service owns. It is needed for the
// inbound direction only: a caller setting a PIN encrypts it so the new PIN
// does not travel in clear text, and only this service can open it.
//
// The pair is generated on first use and lives for the process lifetime, which
// is the right lifetime for a mock: nothing it protects outlives the process.
type serviceKeyPair struct {
	once sync.Once
	key  *rsa.PrivateKey
	err  error
}

var serviceKeys serviceKeyPair

func (s *serviceKeyPair) get() (*rsa.PrivateKey, error) {
	s.once.Do(func() {
		s.key, s.err = rsa.GenerateKey(rand.Reader, 2048)
	})
	return s.key, s.err
}

// servicePublicKeyBase64 returns this service's public key as base64 SPKI, in
// the same shape callers send their own keys in.
func servicePublicKeyBase64() (string, error) {
	key, err := serviceKeys.get()
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

// decryptWithServiceKey opens a base64 PKCS#1 v1.5 ciphertext produced against
// this service's public key.
func decryptWithServiceKey(cipherB64 string) ([]byte, error) {
	key, err := serviceKeys.get()
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cipherB64))
	if err != nil {
		return nil, fmt.Errorf("cypher is not valid base64: %w", err)
	}
	plaintext, err := rsa.DecryptPKCS1v15(rand.Reader, key, raw)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}
	return plaintext, nil
}

// generateUnmaskedPAN derives a stable 16-digit PAN from the card id. A real
// provider returns the same PAN for a card every time, so deriving it by hash
// reproduces that without storing one. An empty card id falls back to random
// digits so the function has no failure mode.
func generateUnmaskedPAN(cardID string) string {
	if cardID == "" {
		return randomDigits(16)
	}
	sum := sha256.Sum256([]byte(cardID))
	digits := make([]byte, 16)
	for i := 0; i < 16; i++ {
		digits[i] = '0' + (sum[i] % 10)
	}
	return string(digits)
}

// generateCardPIN derives a stable 4-digit PIN from the card id, for cards
// whose PIN has never been set explicitly.
func generateCardPIN(cardID string) string {
	if cardID == "" {
		return randomDigits(4)
	}
	// Salted so the PIN is not a prefix of the derived PAN.
	sum := sha256.Sum256([]byte("pin:" + cardID))
	digits := make([]byte, 4)
	for i := 0; i < 4; i++ {
		digits[i] = '0' + (sum[i] % 10)
	}
	return string(digits)
}
