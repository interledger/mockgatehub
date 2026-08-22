package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// The caller-side RSA key pair. Consumers generate one, hand over the public
// half when asking for a token, and decrypt the response with the private half.
type callerKeys struct {
	publicKeyB64 string
	private      *rsa.PrivateKey
}

var panPattern = regexp.MustCompile(`^\d{16}$`)
var pinPattern = regexp.MustCompile(`^\d{4,12}$`)

func (tc *TestContext) callerGeneratesRSAKeyPair() error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return err
	}
	tc.keys = &callerKeys{
		publicKeyB64: base64.StdEncoding.EncodeToString(der),
		private:      key,
	}
	return nil
}

// postCardTokenWithPublicKey mints a token of the given type, supplying the
// caller's public key.
func (tc *TestContext) postCardTokenWithPublicKey(tokenType string) error {
	if tc.keys == nil {
		return fmt.Errorf("no caller key pair generated")
	}
	body := map[string]interface{}{
		"cardId":    tc.cardID,
		"publicKey": tc.keys.publicKeyB64,
	}
	if _, err := tc.sendWithManagedUserHeader("POST", "/cards/v1/token/"+tokenType, body); err != nil {
		return err
	}
	return tc.captureCardToken()
}

// postPinChangeToken mints a pin-change token, which needs no public key
// because nothing is encrypted back to the caller.
func (tc *TestContext) postPinChangeToken() error {
	body := map[string]interface{}{"cardId": tc.cardID}
	if _, err := tc.sendWithManagedUserHeader("POST", "/cards/v1/token/pin-change", body); err != nil {
		return err
	}
	return tc.captureCardToken()
}

func (tc *TestContext) captureCardToken() error {
	var resp struct {
		Token string `json:"token"`
		Links []struct {
			Href   string `json:"href"`
			Method string `json:"method"`
		} `json:"links"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return fmt.Errorf("could not parse token response: %w. Body: %s", err, string(tc.lastResponseBody))
	}
	tc.cardToken = resp.Token
	if len(resp.Links) > 0 {
		tc.cardTokenHref = resp.Links[0].Href
		tc.cardTokenMethod = resp.Links[0].Method
	}
	return nil
}

// tokenLinkIsAbsolute checks the advertised link is a URL the browser can
// follow on its own, rather than a path it would have to resolve.
func (tc *TestContext) tokenLinkIsAbsolute() error {
	if tc.cardTokenHref == "" {
		return fmt.Errorf("no link href captured")
	}
	if !strings.HasPrefix(tc.cardTokenHref, "http://") && !strings.HasPrefix(tc.cardTokenHref, "https://") {
		return fmt.Errorf("expected an absolute URL, got %q", tc.cardTokenHref)
	}
	return nil
}

func (tc *TestContext) tokenLinkPathIs(expected string) error {
	if !strings.HasSuffix(tc.cardTokenHref, expected) {
		return fmt.Errorf("expected link href to end with %q, got %q", expected, tc.cardTokenHref)
	}
	return nil
}

func (tc *TestContext) tokenLinkMethodIs(expected string) error {
	if tc.cardTokenMethod != expected {
		return fmt.Errorf("expected link method %q, got %q", expected, tc.cardTokenMethod)
	}
	return nil
}

// browserFollowsLink calls a sensitive-data endpoint the way a browser does:
// bearer token only, no HMAC headers at all.
func (tc *TestContext) browserFollowsLink(path, token string) error {
	switch token {
	case "the issued token":
		token = tc.cardToken
	case "no token":
		token = ""
	}

	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	// requestRaw sends no HMAC headers, which is the point: these endpoints are
	// reached by a browser that holds only the token.
	_, err := tc.requestRaw(http.MethodGet, path, "", "", headers)
	return err
}

func (tc *TestContext) browserFollowsTheCardDataLink(token string) error {
	return tc.browserFollowsLink("/cards/v1/token/card-data/data", token)
}

func (tc *TestContext) browserFollowsThePinLink(token string) error {
	return tc.browserFollowsLink("/cards/v1/token/pin/data", token)
}

// decryptCypher opens the "cypher" field with the caller's private key, using
// PKCS#1 v1.5 to match what consumers do.
func (tc *TestContext) decryptCypher() ([]byte, error) {
	if tc.keys == nil {
		return nil, fmt.Errorf("no caller key pair generated")
	}

	var resp map[string]string
	if err := json.Unmarshal(tc.lastResponseBody, &resp); err != nil {
		return nil, fmt.Errorf("could not parse response: %w. Body: %s", err, string(tc.lastResponseBody))
	}
	cypher, ok := resp["cypher"]
	if !ok {
		return nil, fmt.Errorf("response has no cypher field: %s", string(tc.lastResponseBody))
	}

	raw, err := base64.StdEncoding.DecodeString(cypher)
	if err != nil {
		return nil, fmt.Errorf("cypher is not base64: %w", err)
	}
	plaintext, err := rsa.DecryptPKCS1v15(rand.Reader, tc.keys.private, raw)
	if err != nil {
		return nil, fmt.Errorf("cypher did not decrypt with the caller's private key: %w", err)
	}
	return plaintext, nil
}

// payloadDecryptsToCardDetails is the behaviour that matters: only the holder
// of the private key can read the card details.
func (tc *TestContext) payloadDecryptsToCardDetails() error {
	plaintext, err := tc.decryptCypher()
	if err != nil {
		return err
	}

	var details struct {
		Pan        string `json:"Pan"`
		ExpiryDate string `json:"ExpiryDate"`
		Cvc2       string `json:"Cvc2"`
	}
	if err := json.Unmarshal(plaintext, &details); err != nil {
		return fmt.Errorf("decrypted payload is not card details: %w. Got: %s", err, string(plaintext))
	}

	if !panPattern.MatchString(details.Pan) {
		return fmt.Errorf("expected a 16-digit PAN, got %q", details.Pan)
	}
	if details.ExpiryDate == "" {
		return fmt.Errorf("decrypted card details carry no expiry date")
	}
	if len(details.Cvc2) != 3 {
		return fmt.Errorf("expected a 3-digit CVC, got %q", details.Cvc2)
	}

	tc.decryptedPAN = details.Pan
	return nil
}

func (tc *TestContext) decryptedPANMatchesPrevious() error {
	previous := tc.decryptedPAN
	if previous == "" {
		return fmt.Errorf("no PAN recorded from an earlier read")
	}
	if err := tc.payloadDecryptsToCardDetails(); err != nil {
		return err
	}
	if tc.decryptedPAN != previous {
		return fmt.Errorf("the same card returned two different PANs: %q then %q", previous, tc.decryptedPAN)
	}
	return nil
}

func (tc *TestContext) noCardDataIsDisclosed() error {
	if strings.Contains(string(tc.lastResponseBody), "cypher") {
		return fmt.Errorf("a rejected request still returned a cypher: %s", string(tc.lastResponseBody))
	}
	return nil
}

// callerSetsTheCardPIN performs the full change-PIN exchange: fetch this
// service's public key, encrypt the new PIN to it, mint a pin-change token and
// post the ciphertext.
func (tc *TestContext) callerSetsTheCardPIN(pin string) error {
	if _, err := tc.requestRaw(http.MethodGet, "/cards/v1/token/pin/public-key", "", "", nil); err != nil {
		return err
	}
	var keyResp struct {
		PublicKey string `json:"publicKey"`
	}
	if err := json.Unmarshal(tc.lastResponseBody, &keyResp); err != nil {
		return fmt.Errorf("could not parse public key response: %w. Body: %s", err, string(tc.lastResponseBody))
	}
	if keyResp.PublicKey == "" {
		return fmt.Errorf("service published no public key")
	}

	cypher, err := encryptToBase64SPKI(keyResp.PublicKey, []byte(pin))
	if err != nil {
		return err
	}

	if err := tc.postPinChangeToken(); err != nil {
		return err
	}
	if tc.lastResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("could not obtain a pin-change token: status %d body %s", tc.lastResponse.StatusCode, string(tc.lastResponseBody))
	}

	body, err := json.Marshal(map[string]string{"cypher": cypher})
	if err != nil {
		return err
	}
	_, err = tc.requestRaw(http.MethodPost, "/cards/v1/token/pin/data", string(body), "application/json",
		map[string]string{"Authorization": "Bearer " + tc.cardToken})
	return err
}

// callerReadsTheCardPIN mints a pin token and follows the link to read it.
func (tc *TestContext) callerReadsTheCardPIN() error {
	if err := tc.postCardTokenWithPublicKey("pin"); err != nil {
		return err
	}
	if tc.lastResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("could not obtain a pin token: status %d body %s", tc.lastResponse.StatusCode, string(tc.lastResponseBody))
	}
	return tc.browserFollowsThePinLink("the issued token")
}

func (tc *TestContext) decryptedPINIs(expected string) error {
	plaintext, err := tc.decryptCypher()
	if err != nil {
		return err
	}
	got := strings.TrimSpace(string(plaintext))
	if got != expected {
		return fmt.Errorf("expected PIN %q, got %q", expected, got)
	}
	return nil
}

func (tc *TestContext) decryptedPINLooksLikeAPIN() error {
	plaintext, err := tc.decryptCypher()
	if err != nil {
		return err
	}
	got := strings.TrimSpace(string(plaintext))
	if !pinPattern.MatchString(got) {
		return fmt.Errorf("expected a numeric PIN, got %q", got)
	}
	return nil
}

// encryptToBase64SPKI mirrors what a consumer does when submitting a new PIN.
func encryptToBase64SPKI(publicKeyB64 string, plaintext []byte) (string, error) {
	der, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return "", fmt.Errorf("public key is not base64: %w", err)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return "", fmt.Errorf("public key is not SPKI: %w", err)
	}
	rsaPub, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("public key is not RSA")
	}
	cipherText, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, plaintext)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(cipherText), nil
}
