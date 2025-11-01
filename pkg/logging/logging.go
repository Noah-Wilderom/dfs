package logging

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	// TODO: make this dynamic
	environment = "development"
)

func New() (*zap.Logger, error) {
	return newZapLogger()
}

func MustNew() *zap.Logger {
	logger, err := newZapLogger()
	if err != nil {
		panic(err)
	}
	return logger
}

func newZapLogger() (*zap.Logger, error) {
	var (
		logCfg  zap.Config
		baseDir = "/var/log/dfs"
	)

	switch strings.ToLower(environment) {
	case "test":
		// No-op logger for tests
		return zap.NewNop(), nil
	case "development", "dev":
		// Development logger with more verbose output
		if cwd, err := os.Getwd(); err == nil {
			baseDir = path.Join(cwd, "logs")
		}

		logCfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		logCfg = zap.NewDevelopmentConfig()
	case "production", "prod":
		// Production logger with structured logging
		if homeDir, err := os.UserHomeDir(); err == nil {
			baseDir = path.Join(homeDir, ".local", "share", "dfs", "logs")
		}
		logCfg = zap.NewProductionConfig()
	default:
		return nil, fmt.Errorf("unknown environment: %s", environment)
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}

	timeNow := time.Now()
	logFilePath := path.Join(baseDir, fmt.Sprintf("%d-%d-%d.log", timeNow.Year(), timeNow.Month(), timeNow.Day()))

	logCfg.OutputPaths = []string{
		"stdout",
		logFilePath,
	}

	return logCfg.Build()
}
