package logging

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/DeRuina/timberjack"
	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const MaxFileSizeMB = 500

var (
	sharedWriterMu sync.RWMutex
	sharedWriter   io.Writer = os.Stderr
)

type CaddyWriter struct{}

type sharedWriteCloser struct {
	io.Writer
}

func init() {
	caddy.RegisterModule(CaddyWriter{})
}

func (CaddyWriter) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.logging.writers.flowpanel",
		New: func() caddy.Module { return new(CaddyWriter) },
	}
}

func (CaddyWriter) String() string {
	return "FlowPanel rotating log"
}

func (CaddyWriter) WriterKey() string {
	return "flowpanel"
}

func (CaddyWriter) OpenWriter() (io.WriteCloser, error) {
	sharedWriterMu.RLock()
	writer := sharedWriter
	sharedWriterMu.RUnlock()
	return sharedWriteCloser{Writer: writer}, nil
}

func (sharedWriteCloser) Close() error {
	return nil
}

func New(env string) (*zap.Logger, error) {
	return newLogger(env)
}

func NewRotating(env, path string) (*zap.Logger, io.Closer, error) {
	writer := &timberjack.Logger{
		Filename:    path,
		MaxSize:     MaxFileSizeMB,
		MaxBackups:  10,
		Compression: "gzip",
		FileMode:    0o644,
	}

	sharedWriterMu.Lock()
	sharedWriter = writer
	sharedWriterMu.Unlock()

	logger, err := newLogger(env, zap.WrapCore(func(zapcore.Core) zapcore.Core {
		return zapcore.NewCore(logEncoder(env), zapcore.AddSync(writer), logLevel(env))
	}))
	if err != nil {
		_ = writer.Close()
		return nil, nil, fmt.Errorf("build rotating logger: %w", err)
	}

	return logger, writer, nil
}

func newLogger(env string, options ...zap.Option) (*zap.Logger, error) {
	var cfg zap.Config

	if env == "production" {
		cfg = zap.NewProductionConfig()
	} else {
		cfg = zap.NewDevelopmentConfig()
	}

	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	return cfg.Build(options...)
}

func logEncoder(env string) zapcore.Encoder {
	if env == "production" {
		cfg := zap.NewProductionEncoderConfig()
		cfg.EncodeTime = zapcore.ISO8601TimeEncoder
		return zapcore.NewJSONEncoder(cfg)
	}

	cfg := zap.NewDevelopmentEncoderConfig()
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	return zapcore.NewConsoleEncoder(cfg)
}

func logLevel(env string) zapcore.LevelEnabler {
	if env == "production" {
		return zap.InfoLevel
	}
	return zap.DebugLevel
}
