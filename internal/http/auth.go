package http

import (
	"net/http"
	"strings"

	"drawingService/pkg/httperror"

	"github.com/go-www/silverlining"
)

const (
	HeaderServiceToken = "X-Service-Token"
	HeaderUserEmail    = "X-User-Email"
	HeaderUserLogin    = "X-User-Login"
)

type Caller struct {
	Email string
	Login string
}

type AuthConfig struct {
	ServiceToken string
	AllowedUsers []string
	AllowAnyUser bool
}

func RequireServiceToken(auth AuthConfig) func(silverlining.Handler) silverlining.Handler {
	return func(next silverlining.Handler) silverlining.Handler {
		return func(ctx *silverlining.Context) {
			token, _ := ctx.RequestHeaders().Get(HeaderServiceToken)
			if token == "" || token != auth.ServiceToken {
				httperror.Write(ctx, http.StatusUnauthorized, "invalid service token")
				return
			}
			next(ctx)
		}
	}
}

func ReadCaller(ctx *silverlining.Context) (Caller, error) {
	email, _ := ctx.RequestHeaders().Get(HeaderUserEmail)
	login, _ := ctx.RequestHeaders().Get(HeaderUserLogin)
	if strings.TrimSpace(email) == "" && strings.TrimSpace(login) == "" {
		return Caller{}, errMissingUser
	}
	return Caller{Email: strings.TrimSpace(email), Login: strings.TrimSpace(login)}, nil
}

func (a AuthConfig) IsAllowed(caller Caller) bool {
	if a.AllowAnyUser {
		return true
	}
	if len(a.AllowedUsers) == 0 {
		return false
	}
	for _, allowed := range a.AllowedUsers {
		if allowed == "*" {
			return true
		}
		if allowed != "" && (allowed == caller.Email || allowed == caller.Login) {
			return true
		}
	}
	return false
}
