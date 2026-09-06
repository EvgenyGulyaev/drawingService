package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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
	auth    AuthConfig
	service *service.DrawingService
	pinger  Pinger
	game    func(*silverlining.Context)
}

type Pinger interface {
	Ping(ctx context.Context) error
}

func NewHandler(auth AuthConfig, svc *service.DrawingService, pinger Pinger) *Handler {
	return &Handler{auth: auth, service: svc, pinger: pinger}
}

func (h *Handler) Serve(ctx *silverlining.Context) {
	if h.game != nil && strings.HasPrefix(string(ctx.Path()), "/api/game/") {
		h.game(ctx)
		return
	}
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
		case path == "/internal/drawing/stamps":
			h.listStamps(c)
		case len(parts) == 5 && parts[0] == "internal" && parts[1] == "drawing" && parts[2] == "images" && parts[4] == "content":
			h.downloadImage(c, parts[3])
		case len(parts) == 5 && parts[0] == "internal" && parts[1] == "drawing" && parts[2] == "stamps" && parts[4] == "content":
			h.downloadStampImage(c, parts[3])
		case len(parts) == 4 && parts[0] == "internal" && parts[1] == "drawing" && parts[2] == "images":
			h.getImage(c, parts[3])
		case len(parts) == 4 && parts[0] == "internal" && parts[1] == "drawing" && parts[2] == "stamps":
			h.getStamp(c, parts[3])
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
		if path == "/internal/drawing/stamps" {
			h.createStamp(c)
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
		if len(parts) == 4 && parts[0] == "internal" && parts[1] == "drawing" && parts[2] == "stamps" {
			h.updateStamp(c, parts[3])
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
		if len(parts) == 4 && parts[0] == "internal" && parts[1] == "drawing" && parts[2] == "stamps" {
			h.deleteStamp(c, parts[3])
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
	hctx, hcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer hcancel()
	items, err := h.service.List(hctx)
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

func (h *Handler) listStamps(ctx *silverlining.Context) {
	caller, err := ReadCaller(ctx)
	if err != nil {
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !h.auth.IsAllowed(caller) {
		httperror.Write(ctx, http.StatusForbidden, "user not allowed")
		return
	}
	hctx, hcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer hcancel()
	items, err := h.service.ListStamps(hctx)
	if err != nil {
		httperror.Write(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ctx.WriteJSON(http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) getStamp(ctx *silverlining.Context, id string) {
	caller, err := ReadCaller(ctx)
	if err != nil {
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !h.auth.IsAllowed(caller) {
		httperror.Write(ctx, http.StatusForbidden, "user not allowed")
		return
	}
	stamp, err := h.service.GetStamp(id)
	if err != nil {
		writeServiceError(ctx, err)
		return
	}
	ctx.WriteJSON(http.StatusOK, stamp)
}

func (h *Handler) downloadStampImage(ctx *silverlining.Context, id string) {
	caller, err := ReadCaller(ctx)
	if err != nil {
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !h.auth.IsAllowed(caller) {
		httperror.Write(ctx, http.StatusForbidden, "user not allowed")
		return
	}
	hctx, hcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer hcancel()
	reader, mimeType, err := h.service.DownloadStampImage(hctx, id)
	if err != nil {
		writeServiceError(ctx, err)
		return
	}
	defer reader.Close()
	ctx.ResponseHeaders().Set("Content-Type", mimeType)
	if err := ctx.WriteStream(http.StatusOK, reader); err != nil {
		_ = err
	}
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
	hctx, hcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer hcancel()
	reader, mimeType, err := h.service.Download(hctx, id)
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
		writeMultipartError(ctx, err)
		return
	}
	hctx, hcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer hcancel()
	image, err := h.service.Create(hctx, service.CreateInput{
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
	hctx, hcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer hcancel()
	image, err := h.service.Update(hctx, id, service.UpdateInput{
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
	hctx, hcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer hcancel()
	if err := h.service.Delete(hctx, id); err != nil {
		writeServiceError(ctx, err)
		return
	}
	ctx.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createStamp(ctx *silverlining.Context) {
	caller, err := ReadCaller(ctx)
	if err != nil {
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !h.auth.IsAllowed(caller) {
		httperror.Write(ctx, http.StatusForbidden, "user not allowed")
		return
	}
	input, file, filename, mimeType, err := readMultipartStamp(ctx, h.service.MaxStampBytes())
	if err != nil {
		writeMultipartError(ctx, err)
		return
	}
	hctx, hcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer hcancel()
	stamp, err := h.service.CreateStamp(hctx, service.StampInput{
		Input:    input.Input,
		Filename: filename,
		MimeType: mimeType,
		Body:     file,
		Actor:    actorLabel(caller),
	})
	if err != nil {
		writeServiceError(ctx, err)
		return
	}
	ctx.WriteJSON(http.StatusOK, stamp)
}

func (h *Handler) updateStamp(ctx *silverlining.Context, id string) {
	caller, err := ReadCaller(ctx)
	if err != nil {
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !h.auth.IsAllowed(caller) {
		httperror.Write(ctx, http.StatusForbidden, "user not allowed")
		return
	}
	input, file, filename, mimeType, err := readMultipartStamp(ctx, h.service.MaxStampBytes())
	if err != nil {
		writeMultipartError(ctx, err)
		return
	}
	hctx, hcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer hcancel()
	stamp, err := h.service.UpdateStamp(hctx, id, service.StampInput{
		Input:       input.Input,
		Filename:    filename,
		MimeType:    mimeType,
		Body:        file,
		RemoveImage: input.RemoveImage,
		Actor:       actorLabel(caller),
	})
	if err != nil {
		writeServiceError(ctx, err)
		return
	}
	ctx.WriteJSON(http.StatusOK, stamp)
}

func (h *Handler) deleteStamp(ctx *silverlining.Context, id string) {
	caller, err := ReadCaller(ctx)
	if err != nil {
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !h.auth.IsAllowed(caller) {
		httperror.Write(ctx, http.StatusForbidden, "user not allowed")
		return
	}
	hctx, hcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer hcancel()
	if err := h.service.DeleteStamp(hctx, id); err != nil {
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
			data, err := io.ReadAll(io.LimitReader(part, maxMetadataSize+1))
			if err != nil {
				return model.DrawingImageInput{}, nil, "", "", err
			}
			if len(data) > maxMetadataSize {
				return model.DrawingImageInput{}, nil, "", "", errors.New("metadata too large")
			}
			if err := json.Unmarshal(data, &meta); err != nil {
				return model.DrawingImageInput{}, nil, "", "", fmt.Errorf("metadata: %w", err)
			}
		case "file":
			mimeType = strings.ToLower(strings.TrimSpace(part.Header.Get("Content-Type")))
			if mimeType == "" || mimeType == "application/octet-stream" {
				mimeType = model.DefaultMimeType
			}
			filename = part.FileName()
			fileBytes, err = io.ReadAll(io.LimitReader(part, maxFileBytes+1))
			if err != nil {
				return model.DrawingImageInput{}, nil, "", "", err
			}
			if int64(len(fileBytes)) > maxFileBytes {
				drainRemainingParts(reader)
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

type stampMultipartInput struct {
	Input       model.DrawingStampInput
	RemoveImage bool
}

func readMultipartStamp(ctx *silverlining.Context, maxFileBytes int64) (stampMultipartInput, io.Reader, string, string, error) {
	reader, err := ctx.MultipartReader()
	if err != nil {
		return stampMultipartInput{}, nil, "", "", errors.New("expected multipart/form-data")
	}
	var meta struct {
		Name        string `json:"name"`
		TextValue   string `json:"textValue"`
		Priority    string `json:"priority"`
		RemoveImage bool   `json:"removeImage"`
	}
	var fileBytes []byte
	var filename string
	var mimeType string

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return stampMultipartInput{}, nil, "", "", fmt.Errorf("read multipart: %w", err)
		}
		name := part.FormName()
		switch name {
		case "metadata":
			data, err := io.ReadAll(io.LimitReader(part, maxMetadataSize+1))
			if err != nil {
				return stampMultipartInput{}, nil, "", "", err
			}
			if len(data) > maxMetadataSize {
				return stampMultipartInput{}, nil, "", "", errors.New("metadata too large")
			}
			if err := json.Unmarshal(data, &meta); err != nil {
				return stampMultipartInput{}, nil, "", "", fmt.Errorf("metadata: %w", err)
			}
		case "file":
			mimeType = strings.ToLower(strings.TrimSpace(part.Header.Get("Content-Type")))
			if mimeType == "" || mimeType == "application/octet-stream" {
				mimeType = model.DefaultMimeType
			}
			filename = part.FileName()
			fileBytes, err = io.ReadAll(io.LimitReader(part, maxFileBytes+1))
			if err != nil {
				return stampMultipartInput{}, nil, "", "", err
			}
			if int64(len(fileBytes)) > maxFileBytes {
				drainRemainingParts(reader)
				return stampMultipartInput{}, nil, "", "", service.ErrPayloadTooLarge
			}
		default:
			part.Close()
		}
	}
	input := stampMultipartInput{
		Input: model.DrawingStampInput{
			Name:      meta.Name,
			TextValue: meta.TextValue,
			Priority:  meta.Priority,
		},
		RemoveImage: meta.RemoveImage,
	}
	var body io.Reader
	if fileBytes != nil {
		body = bytes.NewReader(fileBytes)
	}
	return input, body, filename, mimeType, nil
}

const maxMetadataSize = 128 * 1024

func drainRemainingParts(r *multipart.Reader) {
	for {
		p, err := r.NextPart()
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, p)
		p.Close()
	}
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
	case errors.Is(err, service.ErrUnsupportedStampMime):
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrEmptyPayload):
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrPayloadTooLarge):
		httperror.Write(ctx, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, model.ErrStampNameRequired):
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, model.ErrStampNameTooLong):
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, model.ErrStampTextTooLong):
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, model.ErrStampContentRequired):
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, model.ErrStampPriorityInvalid):
		httperror.Write(ctx, http.StatusBadRequest, err.Error())
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
