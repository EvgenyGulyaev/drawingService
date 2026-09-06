package http

import "drawingService/internal/game"

// WithGame adds the independent public game API; existing routes keep their auth.
func (h *Handler) WithGame(handler *game.Handler) *Handler {
	if handler != nil {
		h.game = handler.Serve
	}
	return h
}
