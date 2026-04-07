package ftp

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	ftpserver "github.com/goftp/server"
	"go.uber.org/zap"
)

type Config struct {
	Enabled      bool
	Host         string
	Port         int
	PublicIP     string
	PassivePorts string
}

type Runtime struct {
	logger  *zap.Logger
	service *Service

	mu     sync.Mutex
	server *ftpserver.Server
	config Config
}

func NewRuntime(logger *zap.Logger, service *Service) *Runtime {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Runtime{
		logger:  logger,
		service: service,
	}
}

func (r *Runtime) Apply(ctx context.Context, cfg Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	cfg = normalizeConfig(cfg)
	current := normalizeConfig(r.config)
	if configsEqual(current, cfg) {
		return nil
	}

	if r.server != nil {
		if err := r.server.Shutdown(); err != nil && !errors.Is(err, ftpserver.ErrServerClosed) {
			return err
		}
		r.server = nil
	}

	r.config = cfg
	if !cfg.Enabled {
		r.logger.Info("ftp server disabled")
		return nil
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.Host, intToString(cfg.Port)))
	if err != nil {
		return err
	}

	server := ftpserver.NewServer(&ftpserver.ServerOpts{
		Factory:        &driverFactory{service: r.service},
		Auth:           &authProvider{service: r.service},
		Name:           "FlowPanel FTP",
		Hostname:       cfg.Host,
		Port:           cfg.Port,
		PublicIp:       strings.TrimSpace(cfg.PublicIP),
		PassivePorts:   strings.TrimSpace(cfg.PassivePorts),
		WelcomeMessage: "FlowPanel FTP ready",
		Logger:         &zapLogger{logger: r.logger.Named("ftp")},
	})

	r.server = server
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, ftpserver.ErrServerClosed) {
			r.logger.Error("ftp server stopped unexpectedly", zap.Error(err))
		}
	}()

	r.logger.Info("ftp server listening",
		zap.String("host", cfg.Host),
		zap.Int("port", cfg.Port),
		zap.String("public_ip", strings.TrimSpace(cfg.PublicIP)),
		zap.String("passive_ports", strings.TrimSpace(cfg.PassivePorts)),
	)
	_ = ctx
	return nil
}

func (r *Runtime) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.config = Config{}
	if r.server == nil {
		return nil
	}

	err := r.server.Shutdown()
	r.server = nil
	if errors.Is(err, ftpserver.ErrServerClosed) {
		return nil
	}
	return err
}

func normalizeConfig(cfg Config) Config {
	cfg.Host = strings.TrimSpace(cfg.Host)
	if cfg.Host == "" {
		cfg.Host = DefaultHost()
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultPort()
	}
	cfg.PublicIP = strings.TrimSpace(cfg.PublicIP)
	cfg.PassivePorts = strings.TrimSpace(cfg.PassivePorts)
	if cfg.PassivePorts == "" {
		cfg.PassivePorts = DefaultPassivePorts()
	}
	return cfg
}

func configsEqual(left, right Config) bool {
	return left.Enabled == right.Enabled &&
		left.Host == right.Host &&
		left.Port == right.Port &&
		left.PublicIP == right.PublicIP &&
		left.PassivePorts == right.PassivePorts
}

type authProvider struct {
	service *Service
}

func (a *authProvider) CheckPasswd(username, password string) (bool, error) {
	_, ok, err := a.service.Authenticate(context.Background(), username, password)
	return ok, err
}

type driverFactory struct {
	service *Service
}

func (f *driverFactory) NewDriver() (ftpserver.Driver, error) {
	return &driver{service: f.service}, nil
}

type driver struct {
	service *Service
	conn    *ftpserver.Conn
}

type fileInfo struct {
	os.FileInfo
}

func (f *fileInfo) Owner() string { return "flowpanel" }
func (f *fileInfo) Group() string { return "flowpanel" }

func (d *driver) Init(conn *ftpserver.Conn) {
	d.conn = conn
}

func (d *driver) Stat(requestPath string) (ftpserver.FileInfo, error) {
	fullPath, err := d.resolvePath(requestPath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}

	return &fileInfo{FileInfo: info}, nil
}

func (d *driver) ChangeDir(requestPath string) error {
	fullPath, err := d.resolvePath(requestPath)
	if err != nil {
		return err
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fs.ErrInvalid
	}

	return nil
}

func (d *driver) ListDir(requestPath string, callback func(ftpserver.FileInfo) error) error {
	fullPath, err := d.resolvePath(requestPath)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := callback(&fileInfo{FileInfo: info}); err != nil {
			return err
		}
	}

	return nil
}

func (d *driver) DeleteDir(requestPath string) error {
	fullPath, err := d.resolvePath(requestPath)
	if err != nil {
		return err
	}

	return os.Remove(fullPath)
}

func (d *driver) DeleteFile(requestPath string) error {
	fullPath, err := d.resolvePath(requestPath)
	if err != nil {
		return err
	}

	return os.Remove(fullPath)
}

func (d *driver) Rename(fromPath, toPath string) error {
	sourcePath, err := d.resolvePath(fromPath)
	if err != nil {
		return err
	}
	targetPath, err := d.resolvePath(toPath)
	if err != nil {
		return err
	}

	return os.Rename(sourcePath, targetPath)
}

func (d *driver) MakeDir(requestPath string) error {
	fullPath, err := d.resolvePath(requestPath)
	if err != nil {
		return err
	}

	return os.MkdirAll(fullPath, 0o755)
}

func (d *driver) GetFile(requestPath string, offset int64) (int64, io.ReadCloser, error) {
	fullPath, err := d.resolvePath(requestPath)
	if err != nil {
		return 0, nil, err
	}

	file, err := os.Open(fullPath)
	if err != nil {
		return 0, nil, err
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return 0, nil, err
	}

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		_ = file.Close()
		return 0, nil, err
	}

	return info.Size(), file, nil
}

func (d *driver) PutFile(requestPath string, data io.Reader, appendData bool) (int64, error) {
	fullPath, err := d.resolvePath(requestPath)
	if err != nil {
		return 0, err
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return 0, err
	}

	flags := os.O_CREATE | os.O_WRONLY
	if appendData {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	file, err := os.OpenFile(fullPath, flags, 0o644)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	written, err := io.Copy(file, data)
	return written, err
}

func (d *driver) resolvePath(requestPath string) (string, error) {
	if d == nil || d.conn == nil || d.service == nil {
		return "", fs.ErrPermission
	}

	status, ok, err := d.service.StatusForUsername(context.Background(), d.conn.LoginUser())
	if err != nil {
		return "", err
	}
	if !ok || !status.Supported {
		return "", fs.ErrPermission
	}

	rootPath, err := filepath.Abs(strings.TrimSpace(status.RootPath))
	if err != nil {
		return "", err
	}
	cleaned := strings.TrimPrefix(filepath.Clean(requestPath), string(filepath.Separator))
	fullPath := filepath.Join(rootPath, cleaned)
	resolvedRoot := rootPath + string(filepath.Separator)
	resolvedPath := fullPath
	if !strings.HasPrefix(resolvedPath+string(filepath.Separator), resolvedRoot) && resolvedPath != rootPath {
		return "", fs.ErrPermission
	}

	return fullPath, nil
}

type zapLogger struct {
	logger *zap.Logger
}

func (l *zapLogger) Print(sessionID string, message interface{}) {
	l.logger.Info("ftp", zap.String("session_id", sessionID), zap.Any("message", message))
}

func (l *zapLogger) Printf(sessionID string, format string, v ...interface{}) {
	l.logger.Sugar().Infof("[%s] "+format, append([]interface{}{sessionID}, v...)...)
}

func (l *zapLogger) PrintCommand(sessionID string, command string, params string) {
	if strings.EqualFold(command, "PASS") {
		params = "****"
	}
	l.logger.Debug("ftp command",
		zap.String("session_id", sessionID),
		zap.String("command", command),
		zap.String("params", params),
	)
}

func (l *zapLogger) PrintResponse(sessionID string, code int, message string) {
	l.logger.Debug("ftp response",
		zap.String("session_id", sessionID),
		zap.Int("code", code),
		zap.String("message", message),
	)
}

func intToString(value int) string {
	return strconv.Itoa(value)
}
