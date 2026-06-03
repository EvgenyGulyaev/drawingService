package httperror

import "log"

type Error struct {
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func Write(c interface {
	WriteJSON(status int, body any) error
}, status int, message string) {
	if err := c.WriteJSON(status, Error{Message: message, Status: status}); err != nil {
		log.Print(err)
	}
}
