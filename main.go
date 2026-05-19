package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/firstsano/linko/internal/build"
	"github.com/firstsano/linko/internal/linkoerr"
	"github.com/firstsano/linko/internal/store"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"github.com/natefinch/lumberjack"
	pkgerr "github.com/pkg/errors"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	env := os.Getenv("ENV")
	hostname, _ := os.Hostname()

	logger, closeLogger, err := initializeLogger()
	logger = logger.With(
		slog.String("git_sha", build.GitSHA),
		slog.String("build_time", build.BuildTime),
		slog.String("env", env),
		slog.String("hostname", hostname),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v", err)
		return 1
	}
	defer func() {
		if err := closeLogger(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close logger: %v", err)
		}
	}()

	st, err := store.New(dataDir, logger)
	if err != nil {
		logger.Error("failed to create store", "error", err)
		return 1
	}
	s := newServer(*st, httpPort, cancel, logger)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error("failed to shutdown server", "error", err)
		return 1
	}
	if serverErr != nil {
		logger.Error("server error", "error", err)
		return 1
	}
	return 0
}

type closeFunc func() error
type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}
type multiError interface {
	error
	Unwrap() []error
}

func initializeLogger() (*slog.Logger, closeFunc, error) {
	logFile := os.Getenv("LINKO_LOG_FILE")
	if logFile != "" {
		logger := &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    1,
			MaxAge:     28,
			MaxBackups: 10,
			LocalTime:  false,
			Compress:   true,
		}
		debugHandler := tint.NewHandler(os.Stderr, &tint.Options{
			Level:       slog.LevelDebug,
			ReplaceAttr: replaceAttr,
			NoColor:     !(isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())),
		})
		infoHandler := slog.NewJSONHandler(logger, &slog.HandlerOptions{
			Level:       slog.LevelInfo,
			ReplaceAttr: replaceAttr,
		})
		closeFunc := func() error {
			if err := logger.Close(); err != nil {
				return fmt.Errorf("failed to close log file: %w", err)
			}
			return nil
		}
		slogHandler := slog.NewMultiHandler(debugHandler, infoHandler)

		return slog.New(slogHandler), closeFunc, nil
	}

	closeFunc := func() error {
		return nil
	}
	slogHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	return slog.New(slogHandler), closeFunc, nil
}

func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	var sensitiveKeys = []string{"user", "password", "key", "apikey", "secret", "pin", "creditcardno"}
	if slices.Contains(sensitiveKeys, a.Key) {
		return slog.String(a.Key, "[REDACTED]")
	}

	if a.Value.Kind() == slog.KindString {
		if u, err := url.Parse(a.Value.String()); err == nil {
			if _, hasPassword := u.User.Password(); hasPassword {
				u.User = url.UserPassword(u.User.Username(), "[REDACTED]")
				return slog.String(a.Key, u.String())
			}
		}
	}

	if a.Key != "error" {
		return a
	}

	err, ok := a.Value.Any().(error)
	if !ok {
		return a
	}

	if multiErr, ok := errors.AsType[multiError](err); ok {
		var groupAttrs []slog.Attr
		for i, err := range multiErr.Unwrap() {
			key := fmt.Sprintf("error_%d", i+1)
			errAttrs := slog.GroupAttrs(key, errorAttrs(err)...)
			groupAttrs = append(groupAttrs, errAttrs)
		}
		return slog.GroupAttrs("errors", groupAttrs...)
	}

	return slog.GroupAttrs("error", errorAttrs(err)...)
}

func errorAttrs(err error) []slog.Attr {
	errorAttrs := []slog.Attr{slog.String("message", err.Error())}
	errorAttrs = append(errorAttrs, linkoerr.Attrs(err)...)
	if stackErr, ok := errors.AsType[stackTracer](err); ok {
		errorAttrs = append(errorAttrs, slog.Attr{
			Key:   "stack_trace",
			Value: slog.StringValue(fmt.Sprintf("%+v", stackErr.StackTrace())),
		})
	}

	return errorAttrs
}
