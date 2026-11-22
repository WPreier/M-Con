package ses

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// SignV4 adds an AWS Signature Version 4 Authorization header.
func SignV4(req *http.Request, payload []byte, region, service, accessKey, secretKey string, now time.Time) {
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := hex.EncodeToString(hash(payload))
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if req.Header.Get("Host") == "" {
		req.Header.Set("Host", req.URL.Host)
	}
	signed := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if req.Header.Get("Content-Type") != "" {
		signed = append([]string{"content-type"}, signed...)
	}
	sort.Strings(signed)
	var canonHeaders strings.Builder
	for _, name := range signed {
		canonHeaders.WriteString(name)
		canonHeaders.WriteByte(':')
		canonHeaders.WriteString(strings.TrimSpace(req.Header.Get(http.CanonicalHeaderKey(name))))
		canonHeaders.WriteByte('\n')
	}
	canon := strings.Join([]string{
		req.Method,
		uriPath(req.URL),
		canonicalQuery(req.URL),
		canonHeaders.String(),
		strings.Join(signed, ";"),
		payloadHash,
	}, "\n")
	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(hash([]byte(canon)))
	sig := hex.EncodeToString(hmacSHA(signingKey(secretKey, dateStamp, region, service), []byte(stringToSign)))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+", SignedHeaders="+strings.Join(signed, ";")+", Signature="+sig)
}

func uriPath(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		return "/"
	}
	return p
}

func canonicalQuery(u *url.URL) string {
	if u.RawQuery == "" {
		return ""
	}
	q, _ := url.ParseQuery(u.RawQuery)
	var keys []string
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		values := q[k]
		sort.Strings(values)
		for _, v := range values {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

func signingKey(secret, date, region, service string) []byte {
	kDate := hmacSHA([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA(kDate, []byte(region))
	kService := hmacSHA(kRegion, []byte(service))
	return hmacSHA(kService, []byte("aws4_request"))
}

func hmacSHA(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(data)
	return m.Sum(nil)
}

func hash(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}
