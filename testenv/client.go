package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

func (h *harness) getJSON(path string, out interface{}, headers map[string]string) error {
	req, err := http.NewRequest("GET", mockGatehubURL+path, nil)
	if err != nil {
		return err
	}
	addAuthHeaders(req, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (h *harness) postJSON(path string, body interface{}, headers map[string]string, out interface{}) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", mockGatehubURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	addAuthHeaders(req, payload)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (h *harness) putJSON(path string, body interface{}, headers map[string]string, out interface{}) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", mockGatehubURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	addAuthHeaders(req, payload)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func addAuthHeaders(req *http.Request, body []byte) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := generateSignature(timestamp, req.Method, req.URL.Path, string(body), testAppSecret)
	req.Header.Set("x-gatehub-app-id", testAppID)
	req.Header.Set("x-gatehub-timestamp", timestamp)
	req.Header.Set("x-gatehub-signature", signature)
}

func generateSignature(timestamp, method, path, body, secret string) string {
	message := timestamp + method + path + body
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
