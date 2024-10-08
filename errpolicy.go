package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"

	"github.com/hashicorp/go-multierror"
)

type StrictLogger func(m string)

type contextKey string

func (c contextKey) String() string {
	return string(c)
}

const strictLoggerCtxKey = contextKey("com.dzombak.mqtt2influxdb.ctx.strictLogger")

func NewStrictLogger(strict bool) StrictLogger {
	if strict {
		return func(m string) {
			log.Fatal(m)
		}
	} else {
		return func(m string) {
			log.Println(m)
		}
	}
}

func WithStrictLogger(ctx context.Context, strict bool) context.Context {
	return context.WithValue(ctx, strictLoggerCtxKey, NewStrictLogger(strict))
}

func StrictLoggerFromContext(ctx context.Context) StrictLogger {
	if ctx == nil {
		panic("nil context passed to StrictLoggerFromContext")
	}
	if logger, ok := ctx.Value(strictLoggerCtxKey).(StrictLogger); ok {
		return logger
	} else {
		panic("no StrictLogger found in given context")
	}
}

func SetStrictEnvPolicies(strict bool) {
	if strict {
		_ = os.Setenv("FIELDTAG_DETERMINATION_FAILURE", "fatal")
		_ = os.Setenv("CAST_FAILURE", "fatal")
	}
}

func OnFieldTagDeterminationFailure(err error) {
	onFailureWithPolicy(err, os.Getenv("FIELDTAG_DETERMINATION_FAILURE"))
}

func OnCastFailure(err error) {
	onFailureWithPolicy(err, os.Getenv("CAST_FAILURE"))
}

func onFailureWithPolicy(err error, policy string) {
	switch strings.ToLower(policy) {
	case "ignore":
		return
	case "fatal":
		log.Fatalln(err)
	case "log":
		fallthrough
	default:
		log.Println(err)
	}
}

func ForEachError(err error, fn func(error)) {
	if err == nil {
		return
	}
	var merr *multierror.Error
	if errors.As(err, &merr) {
		for _, err := range merr.Errors {
			ForEachError(err, fn)
		}
		return
	} else {
		fn(err)
	}
}

func IsPartialFailure(err error) bool {
	if err == nil {
		return true
	}
	var merr *multierror.Error
	if errors.As(err, &merr) {
		for _, err := range merr.Errors {
			if !IsPartialFailure(err) {
				return false
			}
		}
		return true
	} else {
		return errors.Is(err, ErrCastFailure) || errors.Is(err, ErrFieldTagDeterminationFailure)
	}
}
