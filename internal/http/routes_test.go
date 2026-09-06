package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net"
	nethttp "net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"drawingService/internal/game"
	"drawingService/internal/google"
	"drawingService/internal/model"
	"drawingService/internal/service"
	"drawingService/internal/store"

	"github.com/go-www/silverlining"
)

const testServiceToken = "test-token"

type testEnv struct {
	baseURL string
	storage *google.FakeStorage
}

func newTestEnv(t *testing.T, allowed string, maxBytes int64) *testEnv {
	t.Helper()
	dir := t.TempDir()
	db, err := store.OpenDb(filepath.Join(dir, "drawings.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
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
	h := NewHandler(auth, svc, storage)
	gameHandler, err := game.New(db.DB)
	if err != nil {
		t.Fatalf("init game: %v", err)
	}
	h.WithGame(gameHandler)

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
	return &testEnv{baseURL: baseURL, storage: storage}
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

func buildStampMultipart(t *testing.T, meta map[string]any, contentType, filename string, fileContent []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("metadata", mustJSON(t, meta)); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if fileContent != nil {
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
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 10, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 10, B: 20, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

func TestStampRoutesCreateListContentUpdateDelete(t *testing.T) {
	env := newTestEnv(t, "*", 0)

	body, ct := buildStampMultipart(t, map[string]any{
		"name":      "  Евгений  ",
		"textValue": "  Evgeny  ",
		"priority":  "text",
	}, "", "", nil)
	resp, data := env.doRequest(t, nethttp.MethodPost, "/internal/drawing/stamps", testServiceToken, "user@example.com", "user", body, ct)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("expected 200 create text stamp, got %d: %s", resp.StatusCode, string(data))
	}
	var created model.DrawingStamp
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatalf("decode stamp: %v", err)
	}
	if created.Name != "Евгений" || created.TextValue != "Evgeny" || created.HasImage {
		t.Fatalf("unexpected created stamp: %#v", created)
	}

	resp, data = env.doRequest(t, nethttp.MethodGet, "/internal/drawing/stamps", testServiceToken, "user@example.com", "user", nil, "")
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("expected 200 list stamps, got %d: %s", resp.StatusCode, string(data))
	}
	var listResp struct {
		Items []model.DrawingStamp `json:"items"`
	}
	if err := json.Unmarshal(data, &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Items) != 1 {
		t.Fatalf("expected one stamp, got %d", len(listResp.Items))
	}

	body, ct = buildStampMultipart(t, map[string]any{
		"name":      "Печать",
		"textValue": "Seal",
		"priority":  "image",
	}, "image/png", "seal.png", tinyPNG(t))
	resp, data = env.doRequest(t, nethttp.MethodPut, "/internal/drawing/stamps/"+created.ID, testServiceToken, "user@example.com", "user", body, ct)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("expected 200 update image stamp, got %d: %s", resp.StatusCode, string(data))
	}
	var updated model.DrawingStamp
	if err := json.Unmarshal(data, &updated); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if !updated.HasImage || updated.Priority != model.StampPriorityImage {
		t.Fatalf("expected image priority stamp, got %#v", updated)
	}

	resp, data = env.doRequest(t, nethttp.MethodGet, "/internal/drawing/stamps/"+created.ID+"/content", testServiceToken, "user@example.com", "user", nil, "")
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("expected 200 stamp content, got %d: %s", resp.StatusCode, string(data))
	}
	if resp.Header.Get("Content-Type") != model.DefaultMimeType {
		t.Fatalf("expected png content type, got %q", resp.Header.Get("Content-Type"))
	}

	resp, _ = env.doRequest(t, nethttp.MethodDelete, "/internal/drawing/stamps/"+created.ID, testServiceToken, "user@example.com", "user", nil, "")
	if resp.StatusCode != nethttp.StatusNoContent {
		t.Fatalf("expected 204 delete stamp, got %d", resp.StatusCode)
	}
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

	resp, _ = env.doRequest(t, nethttp.MethodGet, "/internal/drawing/images/"+created.ID, testServiceToken, "user@example.com", "user", nil, "")
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
	resp, data, err := doMultipartRequestOversize(t, env.baseURL, "/internal/drawing/images", testServiceToken, "user@example.com", "user", body, ct)
	if resp != nil && resp.StatusCode == nethttp.StatusRequestEntityTooLarge {
		return
	}
	// silverlining flushes the 413 response and then closes the connection while the
	// client is still writing the rest of the multipart body, so a broken-pipe write
	// error is the expected outcome here.
	if err != nil && (strings.Contains(err.Error(), "broken pipe") || strings.Contains(err.Error(), "connection reset")) {
		return
	}
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	t.Fatalf("expected 413 or broken-pipe write error, got status=%d err=%v data=%q", status, err, string(data))
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

// doMultipartRequest posts a multipart body and tolerates broken-pipe write errors
// from the client side when the server replies 413 before the full body is sent
// (HTTP/1.1 race that the real browser handles fine).
func doMultipartRequestOversize(t *testing.T, baseURL, path, token, email, login string, body io.Reader, contentType string) (*nethttp.Response, []byte, error) {
	t.Helper()
	req, err := nethttp.NewRequest(nethttp.MethodPost, baseURL+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(HeaderServiceToken, token)
	req.Header.Set(HeaderUserEmail, email)
	req.Header.Set(HeaderUserLogin, login)
	client := &nethttp.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
	if resp == nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data, err
}

func TestListImagesDoesNotLeakDriveFileID(t *testing.T) {
	env := newTestEnv(t, "*", 0)
	body, ct := buildMultipartWithType(t, "secret", "image/png", "test.png", []byte("PNGDATA"))
	resp, data := env.doRequest(t, nethttp.MethodPost, "/internal/drawing/images", testServiceToken, "u@e.com", "u", body, ct)
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("create: %d: %s", resp.StatusCode, string(data))
	}
	var created model.DrawingImage
	json.Unmarshal(data, &created)
	resp, data = env.doRequest(t, nethttp.MethodGet, "/internal/drawing/images", testServiceToken, "u@e.com", "u", nil, "")
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("list: %d", resp.StatusCode)
	}
	if bytes.Contains(data, []byte("drive_file_id")) {
		t.Fatalf("list response leaks drive_file_id: %s", string(data))
	}
	resp, data = env.doRequest(t, nethttp.MethodGet, "/internal/drawing/images/"+created.ID, testServiceToken, "u@e.com", "u", nil, "")
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("get: %d", resp.StatusCode)
	}
	if bytes.Contains(data, []byte("drive_file_id")) {
		t.Fatalf("get response leaks drive_file_id: %s", string(data))
	}
}

func TestHealthzReportsOK(t *testing.T) {
	env := newTestEnv(t, "*", 0)
	resp, data := env.doRequest(t, nethttp.MethodGet, "/healthz", "", "", "", nil, "")
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(data))
	}
}

func TestHealthzReportsUnavailableWhenDriveFails(t *testing.T) {
	env := newTestEnv(t, "*", 0)
	env.storage.SetPingError(errors.New("drive offline"))
	resp, data := env.doRequest(t, nethttp.MethodGet, "/healthz", "", "", "", nil, "")
	if resp.StatusCode != nethttp.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", resp.StatusCode, string(data))
	}
}

func TestRejectsMetadataTooLarge(t *testing.T) {
	env := newTestEnv(t, "*", 0)
	bigTitle := strings.Repeat("a", 130*1024)
	body, ct := buildMultipartWithType(t, bigTitle, "image/png", "test.png", []byte("PNGDATA"))
	resp, data := env.doRequest(t, nethttp.MethodPost, "/internal/drawing/images", testServiceToken, "u@e.com", "u", body, ct)
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, string(data))
	}
}
