package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/pkg/errors"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const UserContextKey contextKey = "user"

var allowedUsers = map[string]string{
	"frodo":   "$2a$10$B6O/n6teuCzpuh66jrUAdeaJ3WvXcxRkzpN0x7H.di9G9e/NGb9Me",
	"samwise": "$2a$10$EWZpvYhUJtJcEMmm/IBOsOGIcpxUnGIVMRiDlN/nxl1RRwWGkJtty",
	// frodo: "ofTheNineFingers"
	// samwise: "theStrong"
	"saruman": "invalidFormat",
}

func (s *server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			httpError(r.Context(), w, errors.New("unauthorized"), http.StatusUnauthorized)
			return
		}
		stored, exists := allowedUsers[username]
		if !exists {
			httpError(r.Context(), w, errors.New("unauthorized"), http.StatusUnauthorized)
			return
		}
		ok, err := s.validatePassword(r.Context(), password, stored)
		if err != nil {
			s.logger.Error(
				"error validating password",
				slog.String("user", username),
				slog.Any("error", err),
			)
			httpError(r.Context(), w, err, http.StatusInternalServerError)
			return
		}
		if !ok {
			httpError(r.Context(), w, errors.New("unauthorized"), http.StatusUnauthorized)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), UserContextKey, username))

		if logContext, ok := r.Context().Value(logContextKey).(*LogContext); ok {
			logContext.Username = username
		}

		next.ServeHTTP(w, r)
	})
}

func (s *server) validatePassword(ctx context.Context, password, stored string) (bool, error) {
	_, span := tracer.Start(ctx, "auth.validate_password")
	defer span.End()

	err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password))
	if err == bcrypt.ErrMismatchedHashAndPassword {
		return false, nil
	}
	if err != nil {
		return false, errors.WithStack(err)
	}
	return true, nil
}
