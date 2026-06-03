package http

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	nethttp "net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"drawingService/internal/google"
	"drawingService/internal/model"
	"drawingService/internal/service"
	"drawingService/internal/store"

	"github.com/go-www/silverlining"
)

const testServiceToken = "test-token"

type testEnv struct {
	baseURL string
}

func newTestEnv(t *testing.T, allowed string, maxBytes int64) *testEnv {
	t.Helper()
	dir := t.TempDir()
	db := store.OpenDb(filepath.Join(dir, "drawings.db"))
	repo := store.NewDrawingRepository(db)
	if err := repo.EnsureBuckets(); err != nil {
		t.Fatalf("ensure buckets: %v", err)
	}
	storage := google.NewFakeStorage()
	svc := service.NewDrawingService(repo, storage, maxBytes)
	auth := AuthConfig{ServiceToken: testServiceToken, AllowAnyUser: allowed == "*"}
	if allowed != "*" {
		auth.AllowedUsers = []string{allowed}
	}
	h := NewHandler(auth, svc)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	baseURL := "http://" + ln.Addr().String()
	srv := &silverlining.Server{Handler: h.Serve}
	go func() {
		_ = srv.Serve(ln)
	}()
	// server stops on test process exit
	return &testEnv{baseURL: baseURL}
}

func (e *testEnv) doRequest(t *testing.T, method, path, token, email, login string, body io.Reader, contentType string) (*nethttp.Response, []byte) {
	t.Helper()
	req, err := nethttp.NewRequest(method, e.baseURL+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set(HeaderServiceToken, token)
	}
	if email != "" {
		req.Header.Set(HeaderUserEmail, email)
	}
	if login != "" {
		req.Header.Set(HeaderUserLogin, login)
	}
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func buildMultipart(t *testing.T, title string, fileContent []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("metadata", mustJSON(t, map[string]any{
		"title":  title,
		"width":  100,
		"height": 50,
	})); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	fw, err := mw.CreateFormFile("file", "test.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(fileContent); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func buildMultipartWithType(t *testing.T, title, contentType, filename string, fileContent []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("metadata", mustJSON(t, map[string]any{
		"title":  title,
		"width":  100,
		"height": 50,
	})); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	hdr := make(textproto.MIMEHeader)
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	hdr.Set("Content-Type", contentType)
	fw, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := fw.Write(fileContent); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

func TestRejectsRequestsWithoutServiceToken(t *testing.T) {
	env := newTestEnv(t, "*", 0)
	resp, _ := env.doRequest(t, nethttp.MethodGet, "/internal/drawing/images", "", "user@example.com", "user", nil, "")
	if resp.StatusCode != nethttp.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestRequiresUserHeaders(t *testing.T) {
	env := newTestEnv(t, "*", 0)
	resp, _ := env.doRequest(t, nethttp.MethodGet, "/internal/drawing/images", testServiceToken, "", "", nil, "")
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateListGetUpdateDeleteFlow(t *testing.T) {
	env := newTestEnv(t, "*", 0)

	body, ct := buildMultipartWithType(t, "  First  ", "image/png", "test.png", []byte("PNGDATA"))
	resp, data := env.doRequest(t, nethttp.MethodPost, "/internal/drawing/images", testServiceToken, "user@example.com", "user", body, ct)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(data))
	}
	var created model.DrawingImage
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Title != "First" {
		t.Fatalf("expected trimmed title, got %q", created.Title)
	}

	resp, data = env.doRequest(t, nethttp.MethodGet, "/internal/drawing/images", testServiceToken, "user@example.com", "user", nil, "")
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("expected 200 list, got %d", resp.StatusCode)
	}
	var listResp struct {
		Items []model.DrawingImage `json:"items"`
	}
	if err := json.Unmarshal(data, &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(listResp.Items))
	}

	resp, data = env.doRequest(t, nethttp.MethodGet, "/internal/drawing/images/"+created.ID, testServiceToken, "user@example.com", "user", nil, "")
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("expected 200 get, got %d", resp.StatusCode)
	}

	body, ct = buildMultipartWithType(t, "Second", "image/png", "test.png", []byte("PNGDATA2"))
	resp, data = env.doRequest(t, nethttp.MethodPut, "/internal/drawing/images/"+created.ID, testServiceToken, "user@example.com", "user", body, ct)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("expected 200 update, got %d: %s", resp.StatusCode, string(data))
	}
	var updated model.DrawingImage
	if err := json.Unmarshal(data, &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Title != "Second" {
		t.Fatalf("expected Second, got %q", updated.Title)
	}

	resp, _ = env.doRequest(t, nethttp.MethodDelete, "/internal/drawing/images/"+created.ID, testServiceToken, "user@example.com", "user", nil, "")
	if resp.StatusCode != nethttp.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	resp, _ = env.doRequest(t, nethttp.MethodGet, "/internal/drawing/images/"+created.ID, testServiceToken, "user@example.com", "user", nil, "")
	if resp.StatusCode != nethttp.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestRejectsNonPNGUpload(t *testing.T) {
	env := newTestEnv(t, "*", 0)
	body, ct := buildMultipartWithType(t, "x", "image/jpeg", "test.jpg", []byte("jpegdata"))
	resp, data := env.doRequest(t, nethttp.MethodPost, "/internal/drawing/images", testServiceToken, "user@example.com", "user", body, ct)
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, string(data))
	}
}

func TestRejectsEmptyTitle(t *testing.T) {
	env := newTestEnv(t, "*", 0)
	body, ct := buildMultipartWithType(t, "   ", "image/png", "test.png", []byte("PNGDATA"))
	resp, data := env.doRequest(t, nethttp.MethodPost, "/internal/drawing/images", testServiceToken, "user@example.com", "user", body, ct)
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, string(data))
	}
}

func TestRejectsOversize(t *testing.T) {
	env := newTestEnv(t, "*", 1024*1024)
	body, ct := buildMultipartWithType(t, "x", "image/png", "test.png", bytes.Repeat([]byte("a"), 2*1024*1024))
	resp, data := env.doRequest(t, nethttp.MethodPost, "/internal/drawing/images", testServiceToken, "user@example.com", "user", body, ct)
	if resp.StatusCode != nethttp.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", resp.StatusCode, string(data))
	}
}

func TestAllowsByUserWhitelist(t *testing.T) {
	env := newTestEnv(t, "vip@example.com", 0)
	resp, _ := env.doRequest(t, nethttp.MethodGet, "/internal/drawing/images", testServiceToken, "other@example.com", "other", nil, "")
	if resp.StatusCode != nethttp.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	resp, _ = env.doRequest(t, nethttp.MethodGet, "/internal/drawing/images", testServiceToken, "vip@example.com", "vip", nil, "")
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestDownloadReturnsPNGBody(t *testing.T) {
	env := newTestEnv(t, "*", 0)
	body, ct := buildMultipartWithType(t, "x", "image/png", "test.png", []byte("PNGDATA"))
	resp, data := env.doRequest(t, nethttp.MethodPost, "/internal/drawing/images", testServiceToken, "user@example.com", "user", body, ct)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("create: %d: %s", resp.StatusCode, string(data))
	}
	var created model.DrawingImage
	json.Unmarshal(data, &created)

	resp, data = env.doRequest(t, nethttp.MethodGet, "/internal/drawing/images/"+url.PathEscape(created.ID)+"/content", testServiceToken, "user@example.com", "user", nil, "")
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(data))
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "image/png") {
		t.Fatalf("expected image/png content type, got %q", resp.Header.Get("Content-Type"))
	}
}
