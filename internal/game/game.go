// Package game owns private drawing-telephone rooms independently of the drawing gallery.
package game

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/png"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	bolt "go.etcd.io/bbolt"
)

const (
	MaxImageBytes   = 512 * 1024
	maxRooms        = 64
	maxStorageBytes = 256 * 1024 * 1024
	roomTTL         = 24 * time.Hour
)

var roomsKey = []byte("crocodile_rooms_v1")
var metaKey = []byte("crocodile_meta_v1")
var stateKey = []byte("state")
var imagesKey = []byte("images")
var bytesKey = []byte("image_bytes")

type gameError struct {
	status  int
	message string
}

func (e *gameError) Error() string             { return e.message }
func problem(status int, message string) error { return &gameError{status, message} }

type Player struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Ready     bool   `json:"ready"`
	Submitted bool   `json:"submitted"`
}
type member struct {
	Player
	SecretHash string `json:"secretHash"`
}
type Entry struct {
	AuthorID string `json:"authorId"`
	Kind     string `json:"kind"`
	Text     string `json:"text,omitempty"`
	ImageID  string `json:"imageId,omitempty"`
}
type Chain struct {
	OwnerID string  `json:"ownerId"`
	Entries []Entry `json:"entries"`
}
type Task struct {
	Kind    string `json:"kind"`
	Prompt  string `json:"prompt,omitempty"`
	ImageID string `json:"imageId,omitempty"`
}
type RoomView struct {
	Code        string   `json:"code"`
	HostID      string   `json:"hostId"`
	YouID       string   `json:"youId"`
	Players     []Player `json:"players"`
	Status      string   `json:"status"`
	GameID      string   `json:"gameId"`
	Stage       int      `json:"stage"`
	TotalStages int      `json:"totalStages"`
	Version     int      `json:"version"`
	Task        *Task    `json:"task,omitempty"`
	Chains      []Chain  `json:"chains,omitempty"`
}
type Session struct {
	Token string   `json:"token"`
	Room  RoomView `json:"room"`
}
type room struct {
	Code       string
	HostID     string
	Players    []member
	Roster     []Player
	Status     string
	GameID     string
	Stage      int
	Version    int
	Chains     []Chain
	UpdatedAt  time.Time
	ImageBytes int64
}

type Handler struct {
	db       *bolt.DB
	limitsMu sync.Mutex
	limits   map[string]window
}
type window struct {
	start time.Time
	count int
}

func New(db *bolt.DB) (*Handler, error) {
	if db == nil {
		return nil, errors.New("game database is required")
	}
	h := &Handler{db: db, limits: make(map[string]window)}
	err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(roomsKey); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(metaKey)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err = h.Cleanup(time.Now()); err != nil {
		return nil, err
	}
	return h, nil
}

func randomID(n int) string {
	b := make([]byte, n)
	// crypto/rand.Read terminates the process on an unavailable OS RNG in Go 1.25.
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func cleanText(value string, max int) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > max {
		return "", problem(400, "Введите от 1 до "+strconv.Itoa(max)+" символов")
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' {
			return "", problem(400, "Недопустимые символы в тексте")
		}
	}
	return value, nil
}

func loadRoom(tx *bolt.Tx, code string) (*bolt.Bucket, *room, error) {
	b := tx.Bucket(roomsKey).Bucket([]byte(code))
	if b == nil {
		return nil, nil, problem(404, "Комната не найдена или срок её хранения истёк")
	}
	var r room
	if err := json.Unmarshal(b.Get(stateKey), &r); err != nil {
		return nil, nil, err
	}
	if time.Since(r.UpdatedAt) > roomTTL {
		return nil, nil, problem(404, "Срок хранения комнаты истёк")
	}
	return b, &r, nil
}
func saveRoom(b *bolt.Bucket, r *room) error {
	r.Version++
	r.UpdatedAt = time.Now()
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return b.Put(stateKey, data)
}
func identify(r *room, token string) (int, error) {
	if len(token) != 64 {
		return -1, problem(401, "Войдите в комнату заново")
	}
	hash := tokenHash(token)
	for i, p := range r.Players {
		if subtle.ConstantTimeCompare([]byte(p.SecretHash), []byte(hash)) == 1 {
			return i, nil
		}
	}
	return -1, problem(401, "Войдите в комнату заново")
}

// Each stage is a permutation; separate offsets avoid repeating drawers for even N.
func chainIndex(player, stage, n int) int {
	offset := 0
	if stage%2 == 1 {
		offset = (stage-1)/2 + 1
	} else if stage > 0 {
		offset = (stage-2)/2 + 3
	}
	return ((player-offset)%n + n) % n
}
func project(r *room, p int) RoomView {
	v := RoomView{Code: r.Code, HostID: r.HostID, YouID: r.Players[p].ID, Status: r.Status, GameID: r.GameID, Stage: r.Stage, TotalStages: 2 * len(r.Players), Version: r.Version, Players: make([]Player, len(r.Players))}
	for i, player := range r.Players {
		v.Players[i] = player.Player
	}
	if r.Status == "finished" {
		v.Chains = r.Chains
		v.Players = append([]Player(nil), r.Roster...)
		v.TotalStages = 2 * len(r.Roster)
		for i := range v.Players {
			v.Players[i].Submitted = true
		}
	}
	if r.Status == "playing" && !r.Players[p].Submitted {
		v.Task = &Task{Kind: "text"}
		if r.Stage%2 == 1 {
			v.Task.Kind = "drawing"
		}
		if r.Stage > 0 {
			previous := r.Chains[chainIndex(p, r.Stage, len(r.Players))].Entries[r.Stage-1]
			v.Task.Prompt = previous.Text
			v.Task.ImageID = previous.ImageID
		}
	}
	return v
}

func (h *Handler) view(code, token string) (RoomView, error) {
	var v RoomView
	err := h.db.View(func(tx *bolt.Tx) error {
		_, r, err := loadRoom(tx, code)
		if err != nil {
			return err
		}
		p, err := identify(r, token)
		if err != nil {
			return err
		}
		v = project(r, p)
		return nil
	})
	return v, err
}

func (h *Handler) join(code, name string, create bool) (Session, error) {
	name, err := cleanText(name, 24)
	if err != nil {
		return Session{}, err
	}
	if strings.Contains(name, "\n") {
		return Session{}, problem(400, "Имя должно быть в одну строку")
	}
	token := randomID(32)
	player := member{Player: Player{ID: randomID(8), Name: name}, SecretHash: tokenHash(token)}
	var result Session
	err = h.db.Update(func(tx *bolt.Tx) error {
		root := tx.Bucket(roomsKey)
		var b *bolt.Bucket
		var r *room
		if create {
			count := 0
			if err := root.ForEach(func(k, v []byte) error {
				if v == nil {
					count++
				}
				return nil
			}); err != nil {
				return err
			}
			if count >= maxRooms {
				return problem(503, "Все комнаты заняты. Попробуйте позже")
			}
			for {
				code = strings.ToUpper(randomID(4))
				if root.Bucket([]byte(code)) == nil {
					break
				}
			}
			b, err = root.CreateBucket([]byte(code))
			if err != nil {
				return err
			}
			if _, err = b.CreateBucket(imagesKey); err != nil {
				return err
			}
			r = &room{Code: code, HostID: player.ID, Status: "lobby", GameID: randomID(12)}
		} else {
			b, r, err = loadRoom(tx, code)
			if err != nil {
				return err
			}
			if r.Status != "lobby" {
				return problem(409, "Партия уже началась. Дождитесь новой игры")
			}
		}
		if len(r.Players) >= 12 {
			return problem(409, "В комнате уже 12 игроков")
		}
		for i, p := range r.Players {
			if strings.EqualFold(p.Name, name) {
				return problem(409, "Это имя уже занято в комнате")
			}
			r.Players[i].Ready = false
		}
		r.Players = append(r.Players, player)
		if err := saveRoom(b, r); err != nil {
			return err
		}
		result = Session{Token: token, Room: project(r, len(r.Players)-1)}
		return nil
	})
	return result, err
}

type command struct {
	Name   string `json:"name"`
	Ready  *bool  `json:"ready"`
	GameID string `json:"gameId"`
	Stage  *int   `json:"stage"`
	Text   string `json:"text"`
	Image  string `json:"image"`
}

func storageBytes(tx *bolt.Tx) int64 {
	n, _ := strconv.ParseInt(string(tx.Bucket(metaKey).Get(bytesKey)), 10, 64)
	return n
}
func addStorage(tx *bolt.Tx, delta int64) error {
	return tx.Bucket(metaKey).Put(bytesKey, []byte(strconv.FormatInt(storageBytes(tx)+delta, 10)))
}

func validateImage(encoded string) ([]byte, error) {
	if len(encoded) > base64.StdEncoding.EncodedLen(MaxImageBytes) {
		return nil, problem(413, "Рисунок должен быть меньше 512 КБ")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return nil, problem(400, "Нужен рисунок в формате PNG")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 2048 || cfg.Height > 2048 || cfg.Width*cfg.Height > 2*1024*1024 {
		return nil, problem(400, "Недопустимый PNG или размер рисунка")
	}
	if _, err = png.Decode(bytes.NewReader(data)); err != nil {
		return nil, problem(400, "Повреждённый PNG")
	}
	return data, nil
}

func (h *Handler) change(code, token, action string, in command) (RoomView, error) {
	// Reject strangers before doing potentially expensive image decoding.
	if _, err := h.view(code, token); err != nil {
		return RoomView{}, err
	}
	var imageData []byte
	var err error
	if action == "submit" && in.Image != "" {
		imageData, err = validateImage(in.Image)
		if err != nil {
			return RoomView{}, err
		}
	}
	var v RoomView
	err = h.db.Update(func(tx *bolt.Tx) error {
		b, r, err := loadRoom(tx, code)
		if err != nil {
			return err
		}
		p, err := identify(r, token)
		if err != nil {
			return err
		}
		switch action {
		case "ready":
			if r.Status != "lobby" {
				return problem(409, "Игра уже началась")
			}
			if in.Ready == nil {
				return problem(400, "Укажите готовность")
			}
			r.Players[p].Ready = *in.Ready
			all := len(r.Players) >= 3
			for _, player := range r.Players {
				all = all && player.Ready
			}
			if all {
				r.Status = "playing"
				r.Stage = 0
				r.Chains = make([]Chain, len(r.Players))
				r.Roster = make([]Player, len(r.Players))
				for i, player := range r.Players {
					r.Roster[i] = player.Player
					r.Chains[i] = Chain{OwnerID: player.ID, Entries: []Entry{}}
				}
			}
		case "submit":
			if in.GameID != r.GameID || in.Stage == nil || *in.Stage < 0 || *in.Stage >= 2*len(r.Players) || r.Status == "lobby" {
				return problem(409, "Задание изменилось. Обновите комнату")
			}
			stage := *in.Stage
			ci := chainIndex(p, stage, len(r.Players))
			chain := &r.Chains[ci]
			if len(chain.Entries) > stage {
				// A committed stage submission is immutable; retries never add a new turn.
				if chain.Entries[stage].AuthorID != r.Players[p].ID {
					return problem(409, "Это задание уже выполнено")
				}
				v = project(r, p)
				return nil
			}
			if r.Status != "playing" || stage != r.Stage {
				return problem(409, "Этот этап сейчас недоступен")
			}
			entry := Entry{AuthorID: r.Players[p].ID, Kind: "text"}
			if stage%2 == 0 {
				if in.Image != "" {
					return problem(400, "На этом этапе нужно описание")
				}
				entry.Text, err = cleanText(in.Text, 200)
				if err != nil {
					return err
				}
			} else {
				if len(imageData) == 0 || in.Text != "" {
					return problem(400, "На этом этапе нужен рисунок")
				}
				if storageBytes(tx)+int64(len(imageData)) > maxStorageBytes {
					return problem(507, "Хранилище рисунков заполнено. Завершите старые комнаты")
				}
				entry.Kind = "drawing"
				entry.ImageID = randomID(16)
				if err := b.Bucket(imagesKey).Put([]byte(entry.ImageID), imageData); err != nil {
					return err
				}
				r.ImageBytes += int64(len(imageData))
				if err := addStorage(tx, int64(len(imageData))); err != nil {
					return err
				}
			}
			chain.Entries = append(chain.Entries, entry)
			r.Players[p].Submitted = true
			all := true
			for _, player := range r.Players {
				all = all && player.Submitted
			}
			if all {
				if r.Stage == 2*len(r.Players)-1 {
					r.Status = "finished"
				} else {
					r.Stage++
					for i := range r.Players {
						r.Players[i].Submitted = false
					}
				}
			}
		case "restart":
			if r.Players[p].ID != r.HostID {
				return problem(403, "Только хозяин может начать новую партию")
			}
			if in.GameID != r.GameID || r.Status == "lobby" {
				return problem(409, "Партия уже изменена")
			}
			if err := b.DeleteBucket(imagesKey); err != nil {
				return err
			}
			if _, err := b.CreateBucket(imagesKey); err != nil {
				return err
			}
			if err := addStorage(tx, -r.ImageBytes); err != nil {
				return err
			}
			r.ImageBytes = 0
			r.Chains = nil
			r.Roster = nil
			r.Status = "lobby"
			r.Stage = 0
			r.GameID = randomID(12)
			for i := range r.Players {
				r.Players[i].Ready = false
				r.Players[i].Submitted = false
			}
		case "leave":
			if r.Status == "playing" {
				return problem(409, "Дождитесь конца партии или попросите хозяина её завершить")
			}
			r.Players = append(r.Players[:p], r.Players[p+1:]...)
			if len(r.Players) == 0 {
				if err := addStorage(tx, -r.ImageBytes); err != nil {
					return err
				}
				return tx.Bucket(roomsKey).DeleteBucket([]byte(code))
			}
			found := false
			for _, player := range r.Players {
				if player.ID == r.HostID {
					found = true
				}
			}
			if !found {
				r.HostID = r.Players[0].ID
			}
			for i := range r.Players {
				r.Players[i].Ready = false
			}
			return saveRoom(b, r)
		default:
			return problem(404, "Метод игры не найден")
		}
		if err := saveRoom(b, r); err != nil {
			return err
		}
		v = project(r, p)
		return nil
	})
	return v, err
}

func (h *Handler) image(code, token, id string) ([]byte, error) {
	var data []byte
	err := h.db.View(func(tx *bolt.Tx) error {
		b, r, err := loadRoom(tx, code)
		if err != nil {
			return err
		}
		p, err := identify(r, token)
		if err != nil {
			return err
		}
		allowed := r.Status == "finished"
		if task := project(r, p).Task; task != nil && task.ImageID == id && id != "" {
			allowed = true
		}
		if !allowed {
			return problem(403, "Этот рисунок пока скрыт")
		}
		stored := b.Bucket(imagesKey).Get([]byte(id))
		if stored == nil {
			return problem(404, "Рисунок не найден")
		}
		data = bytes.Clone(stored)
		return nil
	})
	return data, err
}

// Cleanup removes rooms idle since their last successful game action, including images.
func (h *Handler) Cleanup(now time.Time) error {
	return h.db.Update(func(tx *bolt.Tx) error {
		root := tx.Bucket(roomsKey)
		var expired [][]byte
		if err := root.ForEach(func(k, v []byte) error {
			if v != nil {
				return nil
			}
			var r room
			if err := json.Unmarshal(root.Bucket(k).Get(stateKey), &r); err != nil {
				return err
			}
			if now.Sub(r.UpdatedAt) > roomTTL {
				expired = append(expired, bytes.Clone(k))
				if err := addStorage(tx, -r.ImageBytes); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		for _, key := range expired {
			if err := root.DeleteBucket(key); err != nil {
				return err
			}
		}
		return nil
	})
}
