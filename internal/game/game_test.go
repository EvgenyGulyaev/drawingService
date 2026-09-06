package game

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-www/silverlining"
	bolt "go.etcd.io/bbolt"
)

type testClient struct {
	base  string
	token string
}

func (c testClient) call(t *testing.T, method, path string, input any, want int) []byte {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	client := &http.Client{Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, resp.StatusCode, want, body)
	}
	return body
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func setup(t *testing.T) (testClient, *Handler, *bolt.DB) {
	t.Helper()
	db, err := bolt.Open(filepath.Join(t.TempDir(), "game.db"), 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &silverlining.Server{Handler: h.Serve, MaxBodySize: 1024 * 1024}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { ln.Close(); db.Close() })
	return testClient{base: "http://" + ln.Addr().String()}, h, db
}

func pngData(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

func roomPlayers(t *testing.T, count int) ([]testClient, RoomView) {
	t.Helper()
	c, _, _ := setup(t)
	joined := decode[Session](t, c.call(t, "POST", "/api/game/rooms", map[string]any{"name": "Игрок 0"}, 201))
	c.token = joined.Token
	players := []testClient{c}
	room := joined.Room
	for i := 1; i < count; i++ {
		next := decode[Session](t, c.call(t, "POST", "/api/game/rooms/"+room.Code+"/join", map[string]any{"name": fmt.Sprintf("Игрок %d", i)}, 201))
		players = append(players, testClient{base: c.base, token: next.Token})
		room = next.Room
	}
	for _, player := range players {
		room = decode[RoomView](t, player.call(t, "POST", "/api/game/rooms/"+room.Code+"/ready", map[string]any{"ready": true}, 200))
	}
	return players, room
}

func TestFullGameEveryPlayerDrawsEveryTopic(t *testing.T) {
	for _, count := range []int{3, 4, 5, 6} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			players, room := roomPlayers(t, count)
			path := "/api/game/rooms/" + room.Code
			if room.Status != "playing" || room.TotalStages != 2*count {
				t.Fatalf("start: %+v", room)
			}
			for stage := 0; stage < 2*count; stage++ {
				for i, player := range players {
					body := player.call(t, "GET", path, nil, 200)
					view := decode[RoomView](t, body)
					if view.Stage != stage || view.Task == nil {
						t.Fatalf("stage %d player %d: %+v", stage, i, view)
					}
					if bytes.Contains(body, []byte("secret")) || bytes.Contains(body, []byte("chains")) || bytes.Contains(body, []byte(players[0].token)) {
						t.Fatal("private state leaked")
					}
					input := map[string]any{"gameId": room.GameID, "stage": stage}
					if view.Task.Kind == "drawing" {
						input["image"] = pngData(t)
					} else {
						input["text"] = fmt.Sprintf("Фраза %d / %d", stage, i)
					}
					player.call(t, "POST", path+"/submit", input, 200)
					// A retry of a committed command is safe even after the stage changes.
					player.call(t, "POST", path+"/submit", input, 200)
				}
			}
			result := decode[RoomView](t, players[0].call(t, "GET", path, nil, 200))
			if result.Status != "finished" || len(result.Chains) != count {
				t.Fatalf("finish: %+v", result)
			}
			for _, chain := range result.Chains {
				if len(chain.Entries) != 2*count {
					t.Fatal("incomplete chain")
				}
				drawn := map[string]bool{}
				for i, entry := range chain.Entries {
					if i > 0 && entry.AuthorID == chain.Entries[i-1].AuthorID {
						t.Fatal("player received own work")
					}
					if entry.Kind == "drawing" {
						if drawn[entry.AuthorID] {
							t.Fatal("repeated drawer")
						}
						drawn[entry.AuthorID] = true
					}
				}
				if len(drawn) != count {
					t.Fatal("not everyone drew topic")
				}
			}
			players[1].call(t, "POST", path+"/restart", map[string]any{"gameId": room.GameID}, 403)
			players[count-1].call(t, "POST", path+"/leave", map[string]any{}, 204)
			afterLeave := decode[RoomView](t, players[0].call(t, "GET", path, nil, 200))
			if afterLeave.TotalStages != 2*count || len(afterLeave.Players) != count {
				t.Fatal("finished history lost original participants")
			}
			reset := decode[RoomView](t, players[0].call(t, "POST", path+"/restart", map[string]any{"gameId": room.GameID}, 200))
			if reset.Status != "lobby" {
				t.Fatal("restart failed")
			}
			players[0].call(t, "POST", path+"/submit", map[string]any{"gameId": room.GameID, "stage": 0, "text": "old"}, 409)
		})
	}
}

func TestAtomicSubmissionsPrivacyAndValidation(t *testing.T) {
	players, room := roomPlayers(t, 4)
	path := "/api/game/rooms/" + room.Code
	stranger := testClient{base: players[0].base, token: "wrong"}
	stranger.call(t, "GET", path, nil, 401)
	players[0].call(t, "POST", path+"/submit", map[string]any{"gameId": room.GameID, "stage": 0, "text": " "}, 400)
	players[0].call(t, "POST", path+"/submit", map[string]any{"gameId": room.GameID, "stage": 1, "image": pngData(t)}, 409)
	var wg sync.WaitGroup
	for _, player := range players {
		wg.Go(func() {
			player.call(t, "POST", path+"/submit", map[string]any{"gameId": room.GameID, "stage": 0, "text": "Секретная тема"}, 200)
		})
	}
	wg.Wait()
	for _, player := range players {
		view := decode[RoomView](t, player.call(t, "GET", path, nil, 200))
		if view.Stage != 1 {
			t.Fatalf("double transition: %d", view.Stage)
		}
	}
	players[0].call(t, "POST", path+"/submit", map[string]any{"gameId": room.GameID, "stage": 1, "image": "invalid"}, 400)
	for _, player := range players {
		player.call(t, "POST", path+"/submit", map[string]any{"gameId": room.GameID, "stage": 1, "image": pngData(t)}, 200)
	}
	view := decode[RoomView](t, players[0].call(t, "GET", path, nil, 200))
	imagePath := path + "/images/" + view.Task.ImageID
	players[0].call(t, "GET", imagePath, nil, 200)
	players[1].call(t, "GET", imagePath, nil, 403)
	stranger.call(t, "GET", imagePath, nil, 401)
}

func TestReadinessLeavePersistenceAndExpiry(t *testing.T) {
	c, h, db := setup(t)
	s := decode[Session](t, c.call(t, "POST", "/api/game/rooms", map[string]any{"name": "Аня"}, 201))
	c.token = s.Token
	path := "/api/game/rooms/" + s.Room.Code
	c.call(t, "POST", path+"/ready", map[string]any{"ready": true}, 200)
	b := decode[Session](t, c.call(t, "POST", path+"/join", map[string]any{"name": "Борис"}, 201))
	view := decode[RoomView](t, c.call(t, "GET", path, nil, 200))
	if view.Players[0].Ready || view.Status != "lobby" {
		t.Fatal("join must reset readiness")
	}
	// Reconstruct handler from the same persistent DB, with no in-memory room state.
	restored, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	view, err = restored.view(s.Room.Code, s.Token)
	if err != nil || view.Code != s.Room.Code {
		t.Fatalf("restore: %+v %v", view, err)
	}
	c.call(t, "POST", path+"/leave", map[string]any{}, 204)
	c.token = b.Token
	view = decode[RoomView](t, c.call(t, "GET", path, nil, 200))
	if view.HostID != view.YouID {
		t.Fatal("host did not transfer")
	}
	if err := h.Cleanup(time.Now().Add(25 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	c.call(t, "GET", path, nil, 404)
}

func TestDatabaseReopenAndCapacityRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	first, err := h.join("", "А", true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.join(first.Room.Code, "Б", false)
	if err != nil {
		t.Fatal(err)
	}
	third, err := h.join(first.Room.Code, "В", false)
	if err != nil {
		t.Fatal(err)
	}
	ready := true
	stage := 0
	for _, s := range []Session{first, second, third} {
		if _, err := h.change(s.Room.Code, s.Token, "ready", command{Ready: &ready}); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []Session{first, second, third} {
		if _, err := h.change(s.Room.Code, s.Token, "submit", command{GameID: first.Room.GameID, Stage: &stage, Text: "Сохранённая тема"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h, err = New(db)
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.view(first.Room.Code, first.Token)
	if err != nil || v.Stage != 1 || v.Task == nil || v.Task.Prompt != "Сохранённая тема" {
		t.Fatalf("reopen lost assignment: %+v %v", v, err)
	}
	if err := db.Update(func(tx *bolt.Tx) error { return addStorage(tx, maxStorageBytes) }); err != nil {
		t.Fatal(err)
	}
	stage = 1
	_, err = h.change(first.Room.Code, first.Token, "submit", command{GameID: first.Room.GameID, Stage: &stage, Image: pngData(t)})
	var ge *gameError
	if !errors.As(err, &ge) || ge.status != 507 {
		t.Fatalf("capacity check: %v", err)
	}
	v, err = h.view(first.Room.Code, first.Token)
	if err != nil || v.Task == nil || v.Players[0].Submitted {
		t.Fatal("failed image upload changed game")
	}
}

func TestLargeDrawingPreservesAuthorizationHeader(t *testing.T) {
	players, room := roomPlayers(t, 3)
	path := "/api/game/rooms/" + room.Code
	for _, player := range players {
		player.call(t, "POST", path+"/submit", map[string]any{"gameId": room.GameID, "stage": 0, "text": "Большой рисунок"}, 200)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	_, _ = rand.Read(img.Pix)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if buf.Len() < 8192 {
		t.Fatal("test image must exceed HTTP reader buffer")
	}
	players[0].call(t, "POST", path+"/submit", map[string]any{"gameId": room.GameID, "stage": 1, "image": base64.StdEncoding.EncodeToString(buf.Bytes())}, 200)
}
