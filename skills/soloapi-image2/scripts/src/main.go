package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.soloapi.cc/v1"
	modelID        = "gpt-image-2"
	displayPrice   = "0.10"
	maxBodyBytes   = 64 << 20
	maxRefBytes    = 15 << 20
	maxReferences  = 4
)

var version = "dev"

type config struct {
	BaseURL string
	APIKey  string
}

type commonOptions struct {
	Prompt     string
	PromptFile string
	Output     string
	Size       string
	Yes        bool
	DryRun     bool
}

type apiResponse struct {
	Data []struct {
		B64JSON string `json:"b64_json"`
		URL     string `json:"url"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

type result struct {
	OK               bool   `json:"ok"`
	Mode             string `json:"mode"`
	Model            string `json:"model"`
	Output           string `json:"output,omitempty"`
	Endpoint         string `json:"endpoint,omitempty"`
	RequestID        string `json:"request_id,omitempty"`
	DryRun           bool   `json:"dry_run"`
	PaidAttempts     int    `json:"paid_attempts"`
	DisplayPriceUSD  string `json:"display_price_usd"`
	BillingCaveat    string `json:"billing_caveat"`
	ReferenceCount   int    `json:"reference_count,omitempty"`
	PromptCharacters int    `json:"prompt_characters"`
}

type apiError struct {
	StatusCode int
	RequestID  string
	Message    string
}

func (e *apiError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("upstream returned HTTP %d (request_id=%s): %s", e.StatusCode, e.RequestID, e.Message)
	}
	return fmt.Sprintf("upstream returned HTTP %d: %s", e.StatusCode, e.Message)
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, http.DefaultClient); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer, client *http.Client) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "check":
		return runCheck(args[1:], stdout)
	case "generate":
		return runGenerate(args[1:], stdout, stderr, client)
	case "edit":
		return runEdit(args[1:], stdout, stderr, client)
	case "version", "--version", "-version":
		_, err := fmt.Fprintln(stdout, version)
		return err
	case "help", "--help", "-h":
		_, err := fmt.Fprintln(stdout, helpText())
		return err
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], helpText())
	}
}

func usageError() error {
	return errors.New(helpText())
}

func helpText() string {
	return `SoloAPI Image2 helper

Usage:
  soloapi-image2 check
  soloapi-image2 generate --prompt <text> --out <file> [--size <size>] (--yes | --dry-run)
  soloapi-image2 edit --prompt <text> --image <file> [--image <file> ...] --out <file> [--size <size>] (--yes | --dry-run)

Environment:
  SOLOAPI_IMAGE2_API_KEY   Required for paid requests; never pass it on the command line.
  SOLOAPI_IMAGE2_BASE_URL  Default: https://api.soloapi.cc/v1

One invocation makes at most one network attempt and requests exactly one image.`
}

func loadConfig(requireKey bool) (config, error) {
	base := strings.TrimSpace(os.Getenv("SOLOAPI_IMAGE2_BASE_URL"))
	if base == "" {
		base = defaultBaseURL
	}
	normalized, err := normalizeBaseURL(base)
	if err != nil {
		return config{}, err
	}
	cfg := config{BaseURL: normalized, APIKey: strings.TrimSpace(os.Getenv("SOLOAPI_IMAGE2_API_KEY"))}
	if requireKey && cfg.APIKey == "" {
		return config{}, errors.New("SOLOAPI_IMAGE2_API_KEY is not configured; ask Codex to save your SoloAPI API Key, then restart Codex")
	}
	return cfg, nil
}

func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid SOLOAPI_IMAGE2_BASE_URL")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(local && u.Scheme == "http") {
		return "", errors.New("SOLOAPI_IMAGE2_BASE_URL must use HTTPS (HTTP is allowed only for localhost tests)")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", errors.New("SOLOAPI_IMAGE2_BASE_URL must not contain credentials, query parameters, or fragments")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

func runCheck(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("check does not accept positional arguments")
	}
	cfg, err := loadConfig(false)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"ok":             cfg.APIKey != "",
		"key_configured": cfg.APIKey != "",
		"base_url":       cfg.BaseURL,
		"model":          modelID,
		"network_called": false,
		"version":        version,
	}
	return writeJSON(stdout, payload)
}

func addCommonFlags(fs *flag.FlagSet, opts *commonOptions) {
	fs.StringVar(&opts.Prompt, "prompt", "", "image prompt")
	fs.StringVar(&opts.PromptFile, "prompt-file", "", "UTF-8 prompt file")
	fs.StringVar(&opts.Output, "out", "", "output image path")
	fs.StringVar(&opts.Size, "size", "auto", "auto, 1024x1024, 1536x1024, or 1024x1536")
	fs.BoolVar(&opts.Yes, "yes", false, "confirm one potentially billable image request")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "validate without contacting the upstream")
}

func prepareCommon(opts *commonOptions) error {
	if opts.Yes && opts.DryRun {
		return errors.New("choose either --yes or --dry-run, not both")
	}
	if !opts.Yes && !opts.DryRun {
		return errors.New("paid requests require --yes; use --dry-run to validate without billing")
	}
	if opts.Prompt != "" && opts.PromptFile != "" {
		return errors.New("choose either --prompt or --prompt-file")
	}
	if opts.PromptFile != "" {
		data, err := os.ReadFile(opts.PromptFile)
		if err != nil {
			return fmt.Errorf("read prompt file: %w", err)
		}
		opts.Prompt = string(data)
	}
	opts.Prompt = strings.TrimSpace(opts.Prompt)
	if opts.Prompt == "" {
		return errors.New("prompt is required")
	}
	if len([]rune(opts.Prompt)) > 12000 {
		return errors.New("prompt exceeds 12000 characters")
	}
	if strings.TrimSpace(opts.Output) == "" {
		return errors.New("--out is required")
	}
	switch opts.Size {
	case "auto", "1024x1024", "1536x1024", "1024x1536":
	default:
		return errors.New("unsupported --size; use auto, 1024x1024, 1536x1024, or 1024x1536")
	}
	if _, err := os.Stat(opts.Output); err == nil {
		return fmt.Errorf("output already exists: %s", opts.Output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output path: %w", err)
	}
	return nil
}

func runGenerate(args []string, stdout, stderr io.Writer, client *http.Client) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts commonOptions
	addCommonFlags(fs, &opts)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("generate does not accept positional arguments")
	}
	if err := prepareCommon(&opts); err != nil {
		return err
	}
	cfg, err := loadConfig(!opts.DryRun)
	if err != nil {
		return err
	}
	endpoint := cfg.BaseURL + "/images/generations"
	baseResult := result{
		Mode: "generate", Model: modelID, Endpoint: endpoint, DryRun: opts.DryRun,
		DisplayPriceUSD: displayPrice, BillingCaveat: "account ledger is authoritative",
		PromptCharacters: len([]rune(opts.Prompt)),
	}
	if opts.DryRun {
		baseResult.OK = true
		return writeJSON(stdout, baseResult)
	}
	fmt.Fprintf(stderr, "NOTICE: sending one paid image request (display price $%s; ledger is authoritative). No automatic retry.\n", displayPrice)
	body := map[string]any{"model": modelID, "prompt": opts.Prompt, "n": 1}
	if opts.Size != "auto" {
		body["size"] = opts.Size
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "soloapi-image2/"+version)
	image, requestID, err := performImageRequest(client, req)
	if err != nil {
		return err
	}
	path, err := saveImage(opts.Output, image)
	if err != nil {
		return err
	}
	baseResult.OK = true
	baseResult.Output = path
	baseResult.RequestID = requestID
	baseResult.PaidAttempts = 1
	return writeJSON(stdout, baseResult)
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func runEdit(args []string, stdout, stderr io.Writer, client *http.Client) error {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts commonOptions
	var images stringList
	addCommonFlags(fs, &opts)
	fs.Var(&images, "image", "reference image path; repeat up to four times")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("edit does not accept positional arguments")
	}
	if err := prepareCommon(&opts); err != nil {
		return err
	}
	if len(images) == 0 || len(images) > maxReferences {
		return fmt.Errorf("edit requires 1 to %d --image values", maxReferences)
	}
	for _, path := range images {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("inspect reference image %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("reference is not a regular file: %s", path)
		}
		if info.Size() > maxRefBytes {
			return fmt.Errorf("reference image exceeds 15 MiB: %s", path)
		}
		if err := validateImageFile(path); err != nil {
			return fmt.Errorf("invalid reference image %s: %w", path, err)
		}
	}
	cfg, err := loadConfig(!opts.DryRun)
	if err != nil {
		return err
	}
	endpoint := cfg.BaseURL + "/images/edits"
	baseResult := result{
		Mode: "edit", Model: modelID, Endpoint: endpoint, DryRun: opts.DryRun,
		DisplayPriceUSD: displayPrice, BillingCaveat: "account ledger is authoritative",
		ReferenceCount: len(images), PromptCharacters: len([]rune(opts.Prompt)),
	}
	if opts.DryRun {
		baseResult.OK = true
		return writeJSON(stdout, baseResult)
	}
	fmt.Fprintf(stderr, "NOTICE: sending one paid image edit request (display price $%s; ledger is authoritative). No automatic retry.\n", displayPrice)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", modelID); err != nil {
		return err
	}
	if err := writer.WriteField("prompt", opts.Prompt); err != nil {
		return err
	}
	if err := writer.WriteField("n", "1"); err != nil {
		return err
	}
	if opts.Size != "auto" {
		if err := writer.WriteField("size", opts.Size); err != nil {
			return err
		}
	}
	for _, path := range images {
		if err := addMultipartFile(writer, "image", path); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "soloapi-image2/"+version)
	image, requestID, err := performImageRequest(client, req)
	if err != nil {
		return err
	}
	path, err := saveImage(opts.Output, image)
	if err != nil {
		return err
	}
	baseResult.OK = true
	baseResult.Output = path
	baseResult.RequestID = requestID
	baseResult.PaidAttempts = 1
	return writeJSON(stdout, baseResult)
}

func addMultipartFile(writer *multipart.Writer, field, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open reference image: %w", err)
	}
	defer f.Close()
	part, err := writer.CreateFormFile(field, filepath.Base(path))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, io.LimitReader(f, maxRefBytes+1)); err != nil {
		return err
	}
	return nil
}

func performImageRequest(client *http.Client, req *http.Request) ([]byte, string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	if clientCopy.Timeout == 0 {
		clientCopy.Timeout = 240 * time.Second
	}
	// A 307/308 can resend a POST body. Refuse redirects so one invocation
	// cannot silently become two potentially billable image requests.
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := clientCopy.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("image request failed; it was not retried because billing state may be unknown: %w", err)
	}
	defer resp.Body.Close()
	requestID := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("request-id"), resp.Header.Get("cf-ray"))
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, requestID, fmt.Errorf("read upstream response: %w", err)
	}
	if len(body) > maxBodyBytes {
		return nil, requestID, errors.New("upstream response exceeds 64 MiB")
	}
	var payload apiResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, requestID, fmt.Errorf("upstream returned invalid JSON (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := http.StatusText(resp.StatusCode)
		if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
			message = sanitizeMessage(payload.Error.Message)
			if auth := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "); auth != "" {
				message = strings.ReplaceAll(message, auth, "[REDACTED]")
			}
		}
		return nil, requestID, &apiError{StatusCode: resp.StatusCode, RequestID: requestID, Message: message}
	}
	if len(payload.Data) == 0 {
		return nil, requestID, errors.New("upstream response contains no image data")
	}
	item := payload.Data[0]
	if item.B64JSON != "" {
		decoded, err := base64.StdEncoding.DecodeString(item.B64JSON)
		if err != nil {
			return nil, requestID, errors.New("upstream returned invalid base64 image data")
		}
		if len(decoded) > maxBodyBytes {
			return nil, requestID, errors.New("decoded image exceeds 64 MiB")
		}
		if err := validateImageBytes(decoded); err != nil {
			return nil, requestID, err
		}
		return decoded, requestID, nil
	}
	if item.URL == "" {
		return nil, requestID, errors.New("upstream image item contains neither b64_json nor url")
	}
	image, err := downloadImage(&clientCopy, item.URL)
	return image, requestID, err
}

func downloadImage(client *http.Client, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, errors.New("upstream returned an invalid image URL")
	}
	if u.User != nil {
		return nil, errors.New("upstream returned an image URL containing credentials")
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("image download returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBodyBytes {
		return nil, errors.New("downloaded image exceeds 64 MiB")
	}
	if err := validateImageBytes(data); err != nil {
		return nil, err
	}
	return data, nil
}

func validateImageBytes(data []byte) error {
	if len(data) < 12 {
		return errors.New("upstream returned an empty or invalid image")
	}
	png := bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	jpeg := bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff})
	webp := bytes.HasPrefix(data, []byte("RIFF")) && len(data) >= 12 && string(data[8:12]) == "WEBP"
	if !png && !jpeg && !webp {
		return errors.New("upstream payload is not a PNG, JPEG, or WebP image")
	}
	return nil
}

func validateImageFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	header := make([]byte, 12)
	if _, err := io.ReadFull(f, header); err != nil {
		return errors.New("file is too small to be a supported image")
	}
	return validateImageBytes(header)
}

func saveImage(path string, data []byte) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create output file: %w", err)
	}
	name := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return "", fmt.Errorf("write output file: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("sync output file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close output file: %w", err)
	}
	ok = true
	return abs, nil
}

func sanitizeMessage(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 500 {
		message = message[:500] + "..."
	}
	return message
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
