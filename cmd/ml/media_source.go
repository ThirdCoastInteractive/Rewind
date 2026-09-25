package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const privateMasterTTL = 15 * time.Minute

var errInvalidPrivateMaster = errors.New("invalid private master source")

// privateMasterURL signs a private R2 object for the media worker. The object
// key comes from the database and is deliberately never treated as a URL.
func privateMasterURL(key, publicBase, secret string, now time.Time) (string, error) {
	key, err := validatePrivateMasterKey(key)
	if err != nil {
		return "", err
	}
	base, err := validateMediaBase(publicBase)
	if err != nil || strings.TrimSpace(secret) == "" {
		return "", errInvalidPrivateMaster
	}
	exp := now.Add(privateMasterTTL).Unix()
	expText := strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("GET\n" + key + "\n" + expText))
	sig := hex.EncodeToString(mac.Sum(nil))

	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	u := strings.TrimRight(base, "/") + "/" + strings.Join(parts, "/")
	return u + "?exp=" + url.QueryEscape(expText) + "&sig=" + url.QueryEscape(sig), nil
}

func validatePrivateMasterKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" || len(key) > 512 || !strings.HasPrefix(key, "org/") ||
		strings.ContainsAny(key, "\\?#%") || strings.Contains(key, "://") {
		return "", errInvalidPrivateMaster
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errInvalidPrivateMaster
		}
		for _, r := range segment {
			if !(r == '-' || r == '_' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return "", errInvalidPrivateMaster
			}
		}
	}
	return key, nil
}

func validateMediaBase(raw string) (string, error) {
	base := strings.TrimSpace(raw)
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errInvalidPrivateMaster
	}
	if port := u.Port(); port != "" && port != "443" {
		return "", errInvalidPrivateMaster
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func configuredPrivateMasterURL(key string, now time.Time) (string, error) {
	base, secret := os.Getenv("R2_PUBLIC_URL"), os.Getenv("R2_SIGNING_SECRET")
	if strings.TrimSpace(base) == "" || strings.TrimSpace(secret) == "" {
		return "", fmt.Errorf("private media signing is not configured: %w", errInvalidPrivateMaster)
	}
	return privateMasterURL(key, base, secret, now)
}
