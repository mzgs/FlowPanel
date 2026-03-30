package caddy

import (
	"context"
	"sync"

	"go.uber.org/zap"
)

type Runtime struct {
	logger          *zap.Logger
	publicHTTPAddr  string
	publicHTTPSAddr string

	mu      sync.Mutex
	started bool
}

func NewRuntime(logger *zap.Logger, publicHTTPAddr, publicHTTPSAddr string) *Runtime {
	return &Runtime{
		logger:          logger,
		publicHTTPAddr:  publicHTTPAddr,
		publicHTTPSAddr: publicHTTPSAddr,
	}
}

func (r *Runtime) Start(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return nil
	}

	r.logger.Info("embedded caddy runtime initialized in skeleton mode",
		zap.String("public_http_addr", r.publicHTTPAddr),
		zap.String("public_https_addr", r.publicHTTPSAddr),
		zap.String("note", "public listeners are not bound until step 7"),
	)

	r.started = true

	return nil
}

func (r *Runtime) Stop(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return nil
	}

	r.started = false
	r.logger.Info("embedded caddy runtime stopped")

	return nil
}
