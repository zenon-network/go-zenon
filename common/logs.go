package common

import (
	"bytes"
	"path/filepath"

	"github.com/inconshreveable/log15"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger is the structured key-value logger used throughout the node;
// it is an alias for the log15 logger interface.
type Logger interface {
	log15.Logger
}

// Per-subsystem loggers. Each is a log15 logger pre-tagged with a
// "module" context key naming its subsystem (FetcherLogger and
// DownloaderLogger additionally carry a "submodule" key under the
// protocol handler). All of them inherit the log15 root handler, so
// InitLogging configures their output globally. Subsystems typically
// refine them further with logger.New(...) to add more context.
var (
	ChainLogger      = log15.New("module", "chain")
	ConsensusLogger  = log15.New("module", "consensus")
	NodeLogger       = log15.New("module", "node")
	P2PLogger        = log15.New("module", "p2p")
	PillarLogger     = log15.New("module", "pillar")
	ProtocolLogger   = log15.New("module", "handler")
	FetcherLogger    = ProtocolLogger.New("submodule", "fetcher")
	DownloaderLogger = ProtocolLogger.New("submodule", "downloader")
	RPCLogger        = log15.New("module", "rpc")
	VerifierLogger   = log15.New("module", "verifier")
	ZenonLogger      = log15.New("module", "zenon")
	VmLogger         = log15.New("module", "vm")
	SupervisorLogger = log15.New("module", "supervisor")
	EmbeddedLogger   = log15.New("module", "embedded")
	WalletLogger     = log15.New("module", "wallet")
)

// InitLogging routes the log15 root handler — and therefore every logger
// in this package — to rotating files under dataPath/log: zenon.log
// receives records at logLevelStr severity and above in logfmt format,
// and error/zenon.error.log receives error records and above. Files rotate at
// 100 MB, keeping at most 14 backups for at most 14 days. An
// unrecognized level string falls back to info.
func InitLogging(dataPath, logLevelStr string) {
	var logHandle []log15.Handler

	logDir := runLogDir(dataPath)
	logLevel, err := log15.LvlFromString(logLevelStr)
	if err != nil {
		logLevel = log15.LvlInfo
	}

	logHandle = append(logHandle, errorExcludeLvlFilterHandler(logLevel, runLogHandler(logDir)))
	logHandle = append(logHandle, log15.LvlFilterHandler(log15.LvlError, runErrorLogHandler(logDir)))

	log15.Root().SetHandler(log15.MultiHandler(
		logHandle...,
	))
}

func runLogDir(dataPath string) string {
	return filepath.Join(dataPath, "log")
}
func runLogHandler(logDir string) log15.Handler {
	filename := "zenon.log"
	logger := defaultLogger(filepath.Join(logDir, filename))
	return log15.StreamHandler(logger, log15.LogfmtFormat())
}
func runErrorLogHandler(logDir string) log15.Handler {
	filename := "zenon.error.log"
	logger := defaultLogger(filepath.Join(logDir, "error", filename))
	return log15.StreamHandler(logger, log15.LogfmtFormat())
}
func errorExcludeLvlFilterHandler(maxLvl log15.Lvl, h log15.Handler) log15.Handler {
	return log15.FilterHandler(func(r *log15.Record) (ss bool) {
		return r.Lvl <= maxLvl
	}, h)
}
func defaultLogger(absFilePath string) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   absFilePath,
		MaxSize:    100,
		MaxBackups: 14,
		MaxAge:     14,
		Compress:   false,
		LocalTime:  false,
	}
}

// LogSaver is a log15 formatter used in tests: it captures formatted
// records in an in-memory buffer and overwrites each record's timestamp
// with the package Clock, so log output is deterministic under the mock
// clock.
type LogSaver struct {
	format log15.Format
	buffer *bytes.Buffer
}

// Format stamps the record with the current Clock time and delegates to
// the wrapped format.
func (f LogSaver) Format(r *log15.Record) []byte {
	r.Time = Clock.Now()
	return f.format.Format(r)
}

// SaveLogs redirects log into an in-memory buffer (replacing its current
// handler) and returns a late-resolving Expecter over the accumulated
// logfmt output, letting tests snapshot-assert everything logged up to
// the point where Equals is called. Record timestamps come from the
// package Clock.
func SaveLogs(log log15.Logger) *Expecter {
	logBuffer := new(bytes.Buffer)
	handler := &LogSaver{format: log15.LogfmtFormat(), buffer: logBuffer}
	log.SetHandler(log15.StreamHandler(logBuffer, handler))
	return LateCaller(func() (string, error) {
		return handler.buffer.String(), nil
	})
}
