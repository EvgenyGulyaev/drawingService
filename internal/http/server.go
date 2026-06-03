package http

import (
	"log"

	"github.com/go-www/silverlining"
)

type Server struct {
	port    string
	handler *Handler
}

func NewServer(port string, handler *Handler) *Server {
	return &Server{port: port, handler: handler}
}

func (s *Server) Start() error {
	log.Printf("drawing service listening on %s", s.port)
	return silverlining.ListenAndServe(s.port, s.handler.Serve)
}
