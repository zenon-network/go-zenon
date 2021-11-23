package common

import (
	"bytes"
	"path/filepath"

	"github.com/inconshreveable/log15"
	"gopkg.in/natefinch/lumberjack.v2"
)

type Logger interface {
	log15.Logger
}

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

type LogSaver struct {
	format log15.Format
	buffer *bytes.Buffer
}

func (f LogSaver) Format(r *log15.Record) []byte {
	r.Time = Clock.Now()
	return f.format.Format(r)
}

func SaveLogs(log log15.Logger) *Expecter {
	logBuffer := new(bytes.Buffer)
	handler := &LogSaver{format: log15.LogfmtFormat(), buffer: logBuffer}
	log.SetHandler(log15.StreamHandler(logBuffer, handler))
	return LateCaller(func() (string, error) {
		return handler.buffer.String(), nil
	})
}
