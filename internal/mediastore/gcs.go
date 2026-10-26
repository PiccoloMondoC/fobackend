// Package mediastore implements private Google Cloud Storage using Cloud Run service identity.
// focodebase/fobackend/internal/mediastore/gcs.go
package mediastore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// GCS uses the documented JSON API and metadata token endpoint, with no new
// module dependencies, credential files, public ACLs, signed URLs or redirects.
// Backend endpoints proxy authorized reads; bucket names never enter responses.
type GCS struct {
	bucket   string
	client   *http.Client
	metadata *http.Client
	mu       sync.Mutex
	token    string
	expiry   time.Time
}

var (
	bucketPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,220}[a-z0-9]$`)
	generationPattern = regexp.MustCompile(`^[1-9][0-9]*$`)
)

func privateClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 12 * time.Second
	transport.Proxy = nil // fixed Google endpoints; never send credentials via env proxy
	return &http.Client{
		Timeout: timeout, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func NewGCS(bucket string) (*GCS, error) {
	if !bucketPattern.MatchString(bucket) || strings.Contains(bucket, "..") {
		return nil, ErrUnavailable
	}
	// The caller's context bounds object streams. A future video upload can
	// select an appropriate budget without changing the business/store boundary.
	return &GCS{bucket: bucket, client: privateClient(0), metadata: privateClient(2 * time.Second)}, nil
}

func (s *GCS) Location() (string, string) { return "gcs", s.bucket }
func (s *GCS) Close() error {
	s.client.CloseIdleConnections()
	s.metadata.CloseIdleConnections()
	return nil
}

func (s *GCS) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Add(time.Minute).Before(s.expiry) {
		return s.token, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token", nil)
	if err != nil {
		return "", ErrUnavailable
	}
	req.Header.Set("Metadata-Flavor", "Google")
	res, err := s.metadata.Do(req)
	if err != nil {
		return "", ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Metadata-Flavor") != "Google" {
		return "", ErrUnavailable
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 16384)).Decode(&token) != nil || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "Bearer") || token.ExpiresIn <= 60 {
		return "", ErrUnavailable
	}
	s.token = token.AccessToken
	s.expiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return s.token, nil
}

func (s *GCS) request(ctx context.Context, method, address, contentType string, body io.Reader) (*http.Response, error) {
	token, err := s.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	if res.StatusCode == 401 {
		s.mu.Lock()
		if s.token == token {
			s.token = ""
		}
		s.mu.Unlock()
	}
	return res, nil
}

func (s *GCS) objectURL(key string) string {
	return "https://storage.googleapis.com/storage/v1/b/" + url.PathEscape(s.bucket) + "/o/" + url.PathEscape("media/"+key)
}

func (s *GCS) Put(ctx context.Context, key, contentType string, source io.Reader) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	q := url.Values{"uploadType": {"media"}, "name": {"media/" + key}, "ifGenerationMatch": {"0"}}
	address := "https://storage.googleapis.com/upload/storage/v1/b/" + url.PathEscape(s.bucket) + "/o?" + q.Encode()
	res, err := s.request(ctx, http.MethodPost, address, contentType, source)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return ErrUnavailable
	}
	// Drain only bounded metadata, never log an upstream body or credential.
	_, err = io.Copy(io.Discard, io.LimitReader(res.Body, 65536))
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *GCS) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !validKey(key) {
		return nil, ErrInvalidKey
	}
	res, err := s.request(ctx, http.MethodGet, s.objectURL(key)+"?alt=media", "", nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == 200 {
		return res.Body, nil
	}
	_ = res.Body.Close()
	if res.StatusCode == 404 {
		return nil, ErrNotFound
	}
	return nil, ErrUnavailable
}

func (s *GCS) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return ErrInvalidKey
	}
	// A generation precondition prevents a concurrent external replacement from
	// being deleted between the metadata read and delete request.
	res, err := s.request(ctx, http.MethodGet, s.objectURL(key), "", nil)
	if err != nil {
		return err
	}
	if res.StatusCode == 404 {
		_ = res.Body.Close()
		return nil
	}
	var object struct {
		Generation string `json:"generation"`
	}
	if res.StatusCode != 200 {
		_ = res.Body.Close()
		return ErrUnavailable
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&object)
	_ = res.Body.Close()
	if err != nil || !generationPattern.MatchString(object.Generation) {
		return ErrUnavailable
	}
	res, err = s.request(ctx, http.MethodDelete, s.objectURL(key)+"?generation="+url.QueryEscape(object.Generation)+"&ifGenerationMatch="+url.QueryEscape(object.Generation), "", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 204 && res.StatusCode != 404 {
		return fmt.Errorf("%w: delete status %d", ErrUnavailable, res.StatusCode)
	}
	return nil
}

// Validate requires bucket-level privacy safeguards before accepting traffic.
// The runtime identity needs storage.buckets.get as well as object create/get/delete.
func (s *GCS) Validate(ctx context.Context) error {
	address := "https://storage.googleapis.com/storage/v1/b/" + url.PathEscape(s.bucket) + "?fields=iamConfiguration"
	res, err := s.request(ctx, http.MethodGet, address, "", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var bucket struct {
		IAM struct {
			Uniform struct {
				Enabled bool `json:"enabled"`
			} `json:"uniformBucketLevelAccess"`
			Prevention string `json:"publicAccessPrevention"`
		} `json:"iamConfiguration"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&bucket) != nil || !bucket.IAM.Uniform.Enabled || bucket.IAM.Prevention != "enforced" {
		return ErrUnavailable
	}
	return nil
}
