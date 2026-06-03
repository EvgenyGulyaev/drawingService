package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"drawingService/internal/model"
	"drawingService/internal/service"
	"drawingService/internal/store"
	"drawingService/pkg/httperror"

	"github.com/go-www/silverlining"
)

type Handler struct {
	auth     AuthConfig
	service  *service.DrawingService
	pinger   Pinger
}

type Pinger interface {
	Ping(ctx context.Context) error
}

func NewHandler(auth AuthConfig, svc *service.DrawingService, pinger Pinger) *Handler {
	return &Handler{auth: auth, service: svc, pinger: pinger}
}

func (h *Handler) Serve(ctx *silverlining.Context) {
	switch ctx.Method() {
	case silverlining.MethodOPTIONS:
		ctx.WriteHeader(http.StatusNoContent)
		return
	case silverlining.MethodGET:
		h.handleGet(ctx, string(ctx.Path()))
	case silverlining.MethodPOST:
		h.handlePost(ctx, string(ctx.Path()))
	case silverlining.MethodPUT:
		h.handlePut(ctx, string(ctx.Path()))
	case silverlining.MethodDELETE:
		h.handleDelete(ctx, string(ctx.Path()))
	default:
		httperror.Write(ctx, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleGet(ctx *silverlining.Context, path string) {
	switch path {
	case "/healthz":
		h.handleHealthz(ctx)
		return
	}
	RequireServiceToken(h.auth)(func(c *silverlining.Context) {
		parts := splitPath(path)
		switch {
		case path == "/internal/drawing/images":
			h.listImages(c)
		case len(parts) == 5 && parts[0] == "internal" && parts[1] == "drawing" && parts[2] == "images" && parts[4] == "content":
			h.downloadImage(c, parts[3])
		case len(parts) == 4 && parts[0] == "internal" && parts[1] == "drawing" && parts[2] == "images":
			h.getImage(c, parts[3])
		default:
			httperror.Write(c, http.StatusNotFound, "not found")
		}
	})(ctx)
}

func (h *Handler) handlePost(ctx *silverlining.Context, path string) {
	RequireServiceToken(h.auth)(func(c *silverlining.Context) {
		if path == "/internal/drawing/images" {
			h.createImage(c)
			return
		}
		httperror.Write(c, http.StatusNotFound, "not found")
	})(ctx)
}

func (h *Handler) handlePut(ctx *silverlining.Context, path string) {
	RequireServiceToken(h.auth)(func(c *silverlining.Context) {
		parts := splitPath(path)
		if len(parts) == 4 && parts[0] == "internal" && parts[1] == "drawing" && parts[2] == "images" {
			h.updateImage(c, parts[3])
			return
		}
		httperror.Write(c, http.StatusNotFound, "not found")
	})(ctx)
}

func (h *Handler) handleDelete(ctx *silverlining.Context, path string) {
	RequireServiceToken(h.auth)(func(c *silverlining.Context) {
		parts := splitPath(path)
		if len(parts) == 4 && parts[0] == "internal" && parts[1] == "drawing" && parts[2] == "images" {
			h.deleteImage(c, parts[3])
			return
		}
		httperror.Write(c, http.StatusNotFound, "not found")
	})(ctx)
}

func (h *Handler) listImages(ctx *silverlining.Context) {
	caller, err := ReadCaller(ctx)
	if err != nil {
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !h.auth.IsAllowed(caller) {
		httperror.Write(ctx, http.StatusForbidden, "user not allowed")
		return
	}
	items, err := h.service.List()
	if err != nil {
		httperror.Write(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ctx.WriteJSON(http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) handleHealthz(ctx *silverlining.Context) {
	if h.pinger == nil {
		ctx.WriteJSON(http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.pinger.Ping(pingCtx); err != nil {
		httperror.Write(ctx, http.StatusServiceUnavailable, "drive unavailable: "+err.Error())
		return
	}
	ctx.WriteJSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) getImage(ctx *silverlining.Context, id string) {
	caller, err := ReadCaller(ctx)
	if err != nil {
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !h.auth.IsAllowed(caller) {
		httperror.Write(ctx, http.StatusForbidden, "user not allowed")
		return
	}
	image, err := h.service.Get(id)
	if err != nil {
		writeServiceError(ctx, err)
		return
	}
	ctx.WriteJSON(http.StatusOK, image)
}

func (h *Handler) downloadImage(ctx *silverlining.Context, id string) {
	caller, err := ReadCaller(ctx)
	if err != nil {
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !h.auth.IsAllowed(caller) {
		httperror.Write(ctx, http.StatusForbidden, "user not allowed")
		return
	}
	reader, mimeType, err := h.service.Download(context.Background(), id)
	if err != nil {
		writeServiceError(ctx, err)
		return
	}
	defer reader.Close()
	ctx.ResponseHeaders().Set("Content-Type", mimeType)
	if err := ctx.WriteStream(http.StatusOK, reader); err != nil {
		// best-effort
		_ = err
	}
}

func (h *Handler) createImage(ctx *silverlining.Context) {
	caller, err := ReadCaller(ctx)
	if err != nil {
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !h.auth.IsAllowed(caller) {
		httperror.Write(ctx, http.StatusForbidden, "user not allowed")
		return
	}
	input, file, filename, mimeType, err := readMultipartDrawing(ctx, h.service.MaxFileBytes())
	if err != nil {
		drainAndWriteError(ctx, err)
		return
	}
	image, err := h.service.Create(context.Background(), service.CreateInput{
		Input:    input,
		Filename: filename,
		MimeType: mimeType,
		Body:     file,
		Actor:    actorLabel(caller),
	})
	if err != nil {
		writeServiceError(ctx, err)
		return
	}
	ctx.WriteJSON(http.StatusOK, image)
}

func (h *Handler) updateImage(ctx *silverlining.Context, id string) {
	caller, err := ReadCaller(ctx)
	if err != nil {
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !h.auth.IsAllowed(caller) {
		httperror.Write(ctx, http.StatusForbidden, "user not allowed")
		return
	}
	input, file, filename, mimeType, err := readMultipartDrawing(ctx, h.service.MaxFileBytes())
	if err != nil {
		writeMultipartError(ctx, err)
		return
	}
	image, err := h.service.Update(context.Background(), id, service.UpdateInput{
		Input:    input,
		Filename: filename,
		MimeType: mimeType,
		Body:     file,
		Actor:    actorLabel(caller),
	})
	if err != nil {
		writeServiceError(ctx, err)
		return
	}
	ctx.WriteJSON(http.StatusOK, image)
}

func (h *Handler) deleteImage(ctx *silverlining.Context, id string) {
	caller, err := ReadCaller(ctx)
	if err != nil {
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !h.auth.IsAllowed(caller) {
		httperror.Write(ctx, http.StatusForbidden, "user not allowed")
		return
	}
	if err := h.service.Delete(context.Background(), id); err != nil {
		writeServiceError(ctx, err)
		return
	}
	ctx.WriteHeader(http.StatusNoContent)
}

func readMultipartDrawing(ctx *silverlining.Context, maxFileBytes int64) (model.DrawingImageInput, io.Reader, string, string, error) {
	reader, err := ctx.MultipartReader()
	if err != nil {
		return model.DrawingImageInput{}, nil, "", "", errors.New("expected multipart/form-data")
	}
	var meta model.DrawingImageInput
	var fileBytes []byte
	var filename string
	var mimeType string

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return model.DrawingImageInput{}, nil, "", "", fmt.Errorf("read multipart: %w", err)
		}
		name := part.FormName()
		switch name {
		case "metadata":
			data, err := io.ReadAll(part)
			if err != nil {
				return model.DrawingImageInput{}, nil, "", "", err
			}
			if err := json.Unmarshal(data, &meta); err != nil {
				return model.DrawingImageInput{}, nil, "", "", fmt.Errorf("metadata: %w", err)
			}
		case "file":
			mimeType = strings.ToLower(strings.TrimSpace(part.Header.Get("Content-Type")))
			if mimeType == "" {
				mimeType = model.DefaultMimeType
			}
			filename = part.FileName()
			fileBytes, err = io.ReadAll(io.LimitReader(part, maxFileBytes+1))
			if err != nil {
				return model.DrawingImageInput{}, nil, "", "", err
			}
			if int64(len(fileBytes)) > maxFileBytes {
				return model.DrawingImageInput{}, nil, "", "", service.ErrPayloadTooLarge
			}
		default:
			part.Close()
		}
	}
	if fileBytes == nil {
		return model.DrawingImageInput{}, nil, "", "", errors.New("file is required")
	}
	return meta, bytes.NewReader(fileBytes), filename, mimeType, nil
}

func writeServiceError(ctx *silverlining.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		httperror.Write(ctx, http.StatusNotFound, "not found")
	case errors.Is(err, model.ErrTitleRequired):
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, model.ErrTitleTooLong):
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrUnsupportedMime):
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrEmptyPayload):
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrPayloadTooLarge):
		httperror.Write(ctx, http.StatusRequestEntityTooLarge, err.Error())
	default:
		httperror.Write(ctx, http.StatusInternalServerError, err.Error())
	}
}

func writeMultipartError(ctx *silverlining.Context, err error) {
	if errors.Is(err, service.ErrPayloadTooLarge) {
		httperror.Write(ctx, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	httperror.Write(ctx, http.StatusBadRequest, err.Error())
}


func drainAndWriteError(ctx *silverlining.Context, err error) {
	// When the multipart payload exceeds the limit, the client may still be uploading the
	// rest of the body. Drain it so the server can flush a clean response instead of
	// breaking the connection mid-upload (HTTP/1.1 race).
	if errors.Is(err, service.ErrPayloadTooLarge) {
		if mr, mrErr := ctx.MultipartReader(); mrErr == nil {
			for {
				p, perr := mr.NextPart()
				if perr != nil {
					break
				}
				_, _ = io.Copy(io.Discard, p)
				p.Close()
			}
		}
		httperror.Write(ctx, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	httperror.Write(ctx, http.StatusBadRequest, err.Error())
}
func actorLabel(caller Caller) string {
	if caller.Email != "" {
		return caller.Email
	}
	return caller.Login
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	for i, part := range parts {
		if unescaped, err := url.PathUnescape(part); err == nil {
			parts[i] = unescaped
		}
	}
	return parts
}

