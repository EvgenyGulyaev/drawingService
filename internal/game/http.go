package game

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net"
	"strings"
	"time"

	"github.com/go-www/silverlining"
)

func (h *Handler) Serve(ctx *silverlining.Context) {
	// silverlining otherwise defers flushing while unread request bytes remain.
	defer ctx.Flush()
	ctx.ResponseHeaders().Set("Cache-Control", "no-store")
	ctx.ResponseHeaders().Set("X-Content-Type-Options", "nosniff")
	path := strings.Split(strings.Trim(string(ctx.Path()), "/"), "/")
	if len(path) < 3 || path[0] != "api" || path[1] != "game" || path[2] != "rooms" {
		h.writeError(ctx, problem(404, "Метод игры не найден"))
		return
	}
	token, _ := ctx.RequestHeaders().Get("Authorization")
	// Header strings borrow silverlining's buffer, reused while streaming a large body.
	token = strings.Clone(strings.TrimPrefix(token, "Bearer "))
	code := ""
	if len(path) > 3 {
		code = strings.ToUpper(path[3])
	}
	if ctx.Method() == silverlining.MethodGET {
		if len(path) == 6 && path[4] == "images" {
			data, err := h.image(code, token, path[5])
			if err != nil {
				h.writeError(ctx, err)
				return
			}
			ctx.ResponseHeaders().Set("Content-Type", "image/png")
			_ = ctx.WriteFullBody(200, data)
			return
		}
		if len(path) != 4 {
			h.writeError(ctx, problem(404, "Метод игры не найден"))
			return
		}
		v, err := h.view(code, token)
		if err != nil {
			h.writeError(ctx, err)
			return
		}
		_ = ctx.WriteJSON(200, v)
		return
	}
	if ctx.Method() != silverlining.MethodPOST {
		h.writeError(ctx, problem(405, "Метод не поддерживается"))
		return
	}
	public := len(path) == 3 || (len(path) == 5 && path[4] == "join")
	if !public && len(path) != 5 {
		h.writeError(ctx, problem(404, "Метод игры не найден"))
		return
	}
	if public && !h.allowGuest(ctx.RemoteAddr().String()) {
		h.writeError(ctx, problem(429, "Слишком много входов. Подождите минуту"))
		return
	}
	contentType, _ := ctx.RequestHeaders().Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		h.writeError(ctx, problem(415, "Ожидается application/json"))
		return
	}
	limit := int64(2048)
	if !public && path[4] == "submit" {
		limit = 720 * 1024
	}
	reader := io.LimitReader(ctx.BodyReader(), limit+1)
	// silverlining 1.3.3 miscounts direct reads larger than its 8 KiB buffer.
	// Hide Buffer.ReadFrom so CopyBuffer always reads at most 4 KiB.
	var body bytes.Buffer
	_, err = io.CopyBuffer(struct{ io.Writer }{&body}, reader, make([]byte, 4096))
	data := body.Bytes()
	if err != nil {
		h.writeError(ctx, problem(400, "Не удалось прочитать запрос"))
		return
	}
	if int64(len(data)) > limit {
		h.writeError(ctx, problem(413, "Запрос слишком большой"))
		return
	}
	var in command
	if err = json.Unmarshal(data, &in); err != nil {
		h.writeError(ctx, problem(400, "Некорректный JSON"))
		return
	}
	if public {
		session, err := h.join(code, in.Name, len(path) == 3)
		if err != nil {
			h.writeError(ctx, err)
			return
		}
		_ = ctx.WriteJSON(201, session)
		return
	}
	v, err := h.change(code, token, path[4], in)
	if err != nil {
		h.writeError(ctx, err)
		return
	}
	if path[4] == "leave" {
		ctx.WriteHeader(204)
		return
	}
	_ = ctx.WriteJSON(200, v)
}

func (h *Handler) writeError(ctx *silverlining.Context, err error) {
	status, message := 500, "Не удалось сохранить игру. Попробуйте ещё раз"
	var gameErr *gameError
	if errors.As(err, &gameErr) {
		status, message = gameErr.status, gameErr.message
	} else {
		log.Printf("crocodile: %v", err)
	}
	_ = ctx.WriteJSON(status, map[string]string{"error": message})
}

func (h *Handler) allowGuest(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	h.limitsMu.Lock()
	defer h.limitsMu.Unlock()
	now := time.Now()
	for key, w := range h.limits {
		if now.Sub(w.start) > time.Minute {
			delete(h.limits, key)
		}
	}
	w := h.limits[host]
	if w.count == 0 {
		if len(h.limits) >= 2048 {
			return false
		}
		w.start = now
	}
	if w.count >= 30 {
		return false
	}
	w.count++
	h.limits[host] = w
	return true
}
