package http

import (
	"log"

	"github.com/go-www/silverlining"
)

type Server struct {
	addr           string
	handler        *Handler
	maxRequestBody int64
}

const defaultMultipartOverheadBytes int64 = 64 * 1024

func NewServer(addr string, handler *Handler) *Server {
	return &Server{addr: addr, handler: handler}
}

func (s *Server) WithMaxRequestBody(n int64) *Server {
	s.maxRequestBody = n
	return s
}

func (s *Server) Start() error {
	maxBody := s.maxRequestBody
	if maxBody <= 0 {
		// Default silverlining MaxBodySize (2MB) is too small for a 10MB drawing upload.
		// We allow up to maxFileBytes plus multipart overhead, then enforce the real cap
		// via io.LimitReader inside the handler so we can return a clean 413.
		maxBody = 2 * (s.handler.service.MaxFileBytes() + defaultMultipartOverheadBytes)
	}
	log.Printf("drawing service: addr=%s maxBody=%d (service.MaxFileBytes=%d)", s.addr, maxBody, s.handler.service.MaxFileBytes())
	srv := &silverlining.Server{
		MaxBodySize: maxBody,
		Handler:     s.handler.Serve,
	}
	ln, err := netListen("tcp", s.addr)
	if err != nil {
		return err
	}
	return srv.Serve(ln)
}
