package httpx

import (
	"bufio"
	"fmt"
	"net"
	stdhttp "net/http"
	"runtime/debug"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

type statusRecorder struct {
	stdhttp.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) Unwrap() stdhttp.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = stdhttp.StatusOK
	}

	written, err := r.ResponseWriter.Write(body)
	r.bytes += written

	return written, err
}

func (r *statusRecorder) Flush() {
	_ = stdhttp.NewResponseController(r.ResponseWriter).Flush()
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if r.status == 0 {
		r.status = stdhttp.StatusSwitchingProtocols
	}

	return stdhttp.NewResponseController(r.ResponseWriter).Hijack()
}

func RequestLogger(logger *zap.Logger) func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(recorder, r)

			statusCode := recorder.status
			if statusCode == 0 {
				statusCode = stdhttp.StatusOK
			}

			logger.Info("http request completed",
				zap.String("request_id", chimiddleware.GetReqID(r.Context())),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("query", r.URL.RawQuery),
				zap.String("remote_ip", r.RemoteAddr),
				zap.Int("status", statusCode),
				zap.Int("bytes", recorder.bytes),
				zap.Duration("duration", time.Since(start)),
				zap.String("user_agent", r.UserAgent()),
			)
		})
	}
}

func Recoverer(logger *zap.Logger) func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("panic recovered",
						zap.String("request_id", chimiddleware.GetReqID(r.Context())),
						zap.String("method", r.Method),
						zap.String("path", r.URL.Path),
						zap.Any("panic", recovered),
						zap.ByteString("stack", debug.Stack()),
					)

					stdhttp.Error(w, fmt.Sprintf("%d %s", stdhttp.StatusInternalServerError, stdhttp.StatusText(stdhttp.StatusInternalServerError)), stdhttp.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
