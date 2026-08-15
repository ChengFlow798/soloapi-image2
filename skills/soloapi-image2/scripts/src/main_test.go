package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

var tinyPNG = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0, 'I', 'E', 'N', 'D'}

func imageJSON() []byte {
	payload := map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(tinyPNG)}}}
	data, _ := json.Marshal(payload)
	return data
}

func TestNormalizeBaseURL(t *testing.T) {
	got, err := normalizeBaseURL("https://api.soloapi.cc/v1/")
	if err != nil || got != "https://api.soloapi.cc/v1" {
		t.Fatalf("unexpected normalization: %q, %v", got, err)
	}
	if _, err := normalizeBaseURL("http://api.soloapi.cc/v1"); err == nil {
		t.Fatal("expected non-local HTTP to be rejected")
	}
	if _, err := normalizeBaseURL("https://user:pass@api.soloapi.cc/v1"); err == nil {
		t.Fatal("expected URL credentials to be rejected")
	}
	if _, err := normalizeBaseURL("http://127.0.0.1:1234/v1"); err != nil {
		t.Fatalf("localhost HTTP should be allowed for tests: %v", err)
	}
}

func TestGenerateMakesOneBoundedRequest(t *testing.T) {
	const fakeKey = "sk-test-not-a-secret"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+fakeKey {
			t.Errorf("unexpected authorization header: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["model"] != modelID || body["n"] != float64(1) || body["prompt"] != "a paper lighthouse" {
			t.Errorf("unexpected body: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "req-mock-generate")
		_, _ = w.Write(imageJSON())
	}))
	defer server.Close()
	t.Setenv("SOLOAPI_IMAGE2_BASE_URL", server.URL+"/v1")
	t.Setenv("SOLOAPI_IMAGE2_API_KEY", fakeKey)
	out := filepath.Join(t.TempDir(), "generated.png")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"generate", "--prompt", "a paper lighthouse", "--out", out, "--yes"}, &stdout, &stderr, server.Client()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one network request, got %d", calls.Load())
	}
	if data, err := os.ReadFile(out); err != nil || !bytes.Equal(data, tinyPNG) {
		t.Fatalf("unexpected output file: %v", err)
	}
	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, fakeKey) {
		t.Fatal("API key leaked to output")
	}
	if !strings.Contains(stdout.String(), `"paid_attempts": 1`) || !strings.Contains(stdout.String(), "req-mock-generate") {
		t.Fatalf("unexpected result: %s", stdout.String())
	}
}

func TestEditSendsRepeatedImageParts(t *testing.T) {
	const fakeKey = "sk-edit-test"
	var imageParts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		reader, err := r.MultipartReader()
		if err != nil {
			t.Errorf("multipart reader: %v", err)
			return
		}
		fields := map[string]string{}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("next part: %v", err)
				return
			}
			data, _ := io.ReadAll(part)
			if part.FormName() == "image" {
				imageParts++
				if !bytes.Equal(data, tinyPNG) {
					t.Errorf("unexpected image part")
				}
			} else {
				fields[part.FormName()] = string(data)
			}
		}
		if fields["model"] != modelID || fields["n"] != "1" || fields["prompt"] != "make it blue" {
			t.Errorf("unexpected multipart fields: %#v", fields)
		}
		_, _ = w.Write(imageJSON())
	}))
	defer server.Close()
	t.Setenv("SOLOAPI_IMAGE2_BASE_URL", server.URL+"/v1")
	t.Setenv("SOLOAPI_IMAGE2_API_KEY", fakeKey)
	tmp := t.TempDir()
	ref1 := filepath.Join(tmp, "one.png")
	ref2 := filepath.Join(tmp, "two.png")
	if err := os.WriteFile(ref1, tinyPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ref2, tinyPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "edited.png")
	var stdout, stderr bytes.Buffer
	err := run([]string{"edit", "--prompt", "make it blue", "--image", ref1, "--image", ref2, "--out", out, "--yes"}, &stdout, &stderr, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if imageParts != 2 {
		t.Fatalf("expected two image parts, got %d", imageParts)
	}
	if strings.Contains(stdout.String()+stderr.String(), fakeKey) {
		t.Fatal("API key leaked to output")
	}
}

func TestDryRunDoesNotNeedKeyOrNetwork(t *testing.T) {
	t.Setenv("SOLOAPI_IMAGE2_API_KEY", "")
	t.Setenv("SOLOAPI_IMAGE2_BASE_URL", "https://api.soloapi.cc/v1")
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, nil
	})}
	var stdout, stderr bytes.Buffer
	out := filepath.Join(t.TempDir(), "dry.png")
	if err := run([]string{"generate", "--prompt", "dry run", "--out", out, "--dry-run"}, &stdout, &stderr, client); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("dry-run contacted the network")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote an output image")
	}
	if !strings.Contains(stdout.String(), `"dry_run": true`) {
		t.Fatalf("unexpected dry-run output: %s", stdout.String())
	}
}

func TestPaidCallRequiresExplicitYes(t *testing.T) {
	t.Setenv("SOLOAPI_IMAGE2_API_KEY", "sk-test")
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, nil
	})}
	var stdout, stderr bytes.Buffer
	err := run([]string{"generate", "--prompt", "test", "--out", filepath.Join(t.TempDir(), "x.png")}, &stdout, &stderr, client)
	if err == nil || !strings.Contains(err.Error(), "require --yes") {
		t.Fatalf("expected confirmation error, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("unconfirmed call contacted the network")
	}
}

func TestEditRejectsNonImageBeforeNetwork(t *testing.T) {
	t.Setenv("SOLOAPI_IMAGE2_API_KEY", "sk-test")
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, nil
	})}
	bad := filepath.Join(t.TempDir(), "not-an-image.png")
	if err := os.WriteFile(bad, []byte("this is not image data"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := run([]string{"edit", "--prompt", "test", "--image", bad, "--out", filepath.Join(t.TempDir(), "x.png"), "--yes"}, &stdout, &stderr, client)
	if err == nil || !strings.Contains(err.Error(), "invalid reference image") {
		t.Fatalf("expected image validation error, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("invalid image caused a network request")
	}
}

func TestNoAutomaticRetryOnServerError(t *testing.T) {
	const fakeKey = "sk-secret-must-be-redacted"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key ` + fakeKey + `; temporarily unavailable"}}`))
	}))
	defer server.Close()
	t.Setenv("SOLOAPI_IMAGE2_BASE_URL", server.URL+"/v1")
	t.Setenv("SOLOAPI_IMAGE2_API_KEY", fakeKey)
	var stdout, stderr bytes.Buffer
	err := run([]string{"generate", "--prompt", "test", "--out", filepath.Join(t.TempDir(), "x.png"), "--yes"}, &stdout, &stderr, server.Client())
	if err == nil {
		t.Fatal("expected upstream error")
	}
	if strings.Contains(err.Error(), fakeKey) || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("expected secret to be redacted, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one attempt, got %d", calls.Load())
	}
}

func TestPaidPostDoesNotFollowRedirect(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/v1/images/generations" {
			http.Redirect(w, r, "/second-paid-request", http.StatusTemporaryRedirect)
			return
		}
		_, _ = w.Write(imageJSON())
	}))
	defer server.Close()
	t.Setenv("SOLOAPI_IMAGE2_BASE_URL", server.URL+"/v1")
	t.Setenv("SOLOAPI_IMAGE2_API_KEY", "sk-no-redirect")
	var stdout, stderr bytes.Buffer
	err := run([]string{"generate", "--prompt", "test", "--out", filepath.Join(t.TempDir(), "x.png"), "--yes"}, &stdout, &stderr, server.Client())
	if err == nil {
		t.Fatal("expected redirect response to fail")
	}
	if calls.Load() != 1 {
		t.Fatalf("expected redirect not to be followed, got %d requests", calls.Load())
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
