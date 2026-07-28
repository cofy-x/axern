package asynclog

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// LogEntry represents a single log entry to be processed asynchronously
type LogEntry struct {
	Level   logrus.Level
	Message string
	Fields  logrus.Fields
	Time    time.Time
}

// AsyncLogger wraps logrus and provides asynchronous logging capabilities
type AsyncLogger struct {
	logger            *logrus.Logger
	logChan           chan *LogEntry
	wg                sync.WaitGroup
	stopOnce          sync.Once
	stopChan          chan struct{}
	bufferSize        int
	droppedCount      uint64
	lastReportedCount uint64
	checkInterval     time.Duration
}

// NewAsyncLogger creates a new async logger with specified buffer size
// Default buffer size is 10000 if bufferSize <= 0
// Default check interval is 30 seconds
func NewAsyncLogger(logger *logrus.Logger, bufferSize int) *AsyncLogger {
	return NewAsyncLoggerWithInterval(logger, bufferSize, 30*time.Second)
}

// NewAsyncLoggerWithInterval creates a new async logger with custom check interval
func NewAsyncLoggerWithInterval(logger *logrus.Logger, bufferSize int, checkInterval time.Duration) *AsyncLogger {
	if bufferSize <= 0 {
		bufferSize = 10000
	}
	if checkInterval <= 0 {
		checkInterval = 30 * time.Second
	}

	al := &AsyncLogger{
		logger:        logger,
		logChan:       make(chan *LogEntry, bufferSize),
		stopChan:      make(chan struct{}),
		bufferSize:    bufferSize,
		checkInterval: checkInterval,
	}

	al.wg.Add(2)
	go al.processLogs()
	go al.monitorDroppedLogs()

	return al
}

// processLogs runs in a goroutine and processes log entries from the channel
func (al *AsyncLogger) processLogs() {
	defer al.wg.Done()

	for {
		select {
		case entry := <-al.logChan:
			al.writeLog(entry)
		case <-al.stopChan:
			// Drain remaining logs before exiting
			for {
				select {
				case entry := <-al.logChan:
					al.writeLog(entry)
				default:
					return
				}
			}
		}
	}
}

// monitorDroppedLogs periodically checks for dropped logs and reports them
func (al *AsyncLogger) monitorDroppedLogs() {
	defer al.wg.Done()

	ticker := time.NewTicker(al.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			currentDropped := atomic.LoadUint64(&al.droppedCount)
			lastReported := atomic.LoadUint64(&al.lastReportedCount)

			if currentDropped > lastReported {
				newDropped := currentDropped - lastReported
				al.logger.Warnf("async logger dropped %d log entries in the last %v (total dropped: %d)",
					newDropped, al.checkInterval, currentDropped)
				atomic.StoreUint64(&al.lastReportedCount, currentDropped)
			}
		case <-al.stopChan:
			return
		}
	}
}

// writeLog writes a log entry to the underlying logger
func (al *AsyncLogger) writeLog(entry *LogEntry) {
	logEntry := al.logger.WithFields(entry.Fields).WithTime(entry.Time)

	switch entry.Level {
	case logrus.DebugLevel:
		logEntry.Debug(entry.Message)
	case logrus.InfoLevel:
		logEntry.Info(entry.Message)
	case logrus.WarnLevel:
		logEntry.Warn(entry.Message)
	case logrus.ErrorLevel:
		logEntry.Error(entry.Message)
	case logrus.FatalLevel:
		logEntry.Fatal(entry.Message)
	case logrus.PanicLevel:
		logEntry.Panic(entry.Message)
	default:
		logEntry.Info(entry.Message)
	}
}

// log sends a log entry to the async channel, drops if buffer is full
func (al *AsyncLogger) log(level logrus.Level, msg string, fields logrus.Fields) {
	entry := &LogEntry{
		Level:   level,
		Message: msg,
		Fields:  fields,
		Time:    time.Now(),
	}

	select {
	case al.logChan <- entry:
		// Successfully queued
	default:
		// Buffer is full, drop the log
		atomic.AddUint64(&al.droppedCount, 1)
	}
}

// GetDroppedCount returns the number of dropped log entries
func (al *AsyncLogger) GetDroppedCount() uint64 {
	return atomic.LoadUint64(&al.droppedCount)
}

// Debug logs a debug message asynchronously
func (al *AsyncLogger) Debug(msg string) {
	al.log(logrus.DebugLevel, msg, nil)
}

// Debugf logs a formatted debug message asynchronously
func (al *AsyncLogger) Debugf(format string, args ...interface{}) {
	al.log(logrus.DebugLevel, fmt.Sprintf(format, args...), nil)
}

// Info logs an info message asynchronously
func (al *AsyncLogger) Info(msg string) {
	al.log(logrus.InfoLevel, msg, nil)
}

// Infof logs a formatted info message asynchronously
func (al *AsyncLogger) Infof(format string, args ...interface{}) {
	al.log(logrus.InfoLevel, fmt.Sprintf(format, args...), nil)
}

// Warn logs a warning message asynchronously
func (al *AsyncLogger) Warn(msg string) {
	al.log(logrus.WarnLevel, msg, nil)
}

// Warnf logs a formatted warning message asynchronously
func (al *AsyncLogger) Warnf(format string, args ...interface{}) {
	al.log(logrus.WarnLevel, fmt.Sprintf(format, args...), nil)
}

// Error logs an error message asynchronously
func (al *AsyncLogger) Error(msg string) {
	al.log(logrus.ErrorLevel, msg, nil)
}

// Errorf logs a formatted error message asynchronously
func (al *AsyncLogger) Errorf(format string, args ...interface{}) {
	al.log(logrus.ErrorLevel, fmt.Sprintf(format, args...), nil)
}

// WithFields returns a logger with additional fields
func (al *AsyncLogger) WithFields(fields logrus.Fields) *AsyncLoggerEntry {
	return &AsyncLoggerEntry{
		logger: al,
		fields: fields,
	}
}

// Close gracefully shuts down the async logger, waiting for all logs to be written
func (al *AsyncLogger) Close() {
	al.stopOnce.Do(func() {
		close(al.stopChan)
		al.wg.Wait()
		close(al.logChan)
	})
}

// AsyncLoggerEntry represents a logger with pre-set fields
type AsyncLoggerEntry struct {
	logger *AsyncLogger
	fields logrus.Fields
}

// Debug logs a debug message with fields
func (e *AsyncLoggerEntry) Debug(msg string) {
	e.logger.log(logrus.DebugLevel, msg, e.fields)
}

// Debugf logs a formatted debug message with fields
func (e *AsyncLoggerEntry) Debugf(format string, args ...interface{}) {
	e.logger.log(logrus.DebugLevel, fmt.Sprintf(format, args...), e.fields)
}

// Info logs an info message with fields
func (e *AsyncLoggerEntry) Info(msg string) {
	e.logger.log(logrus.InfoLevel, msg, e.fields)
}

// Infof logs a formatted info message with fields
func (e *AsyncLoggerEntry) Infof(format string, args ...interface{}) {
	e.logger.log(logrus.InfoLevel, fmt.Sprintf(format, args...), e.fields)
}

// Warn logs a warning message with fields
func (e *AsyncLoggerEntry) Warn(msg string) {
	e.logger.log(logrus.WarnLevel, msg, e.fields)
}

// Warnf logs a formatted warning message with fields
func (e *AsyncLoggerEntry) Warnf(format string, args ...interface{}) {
	e.logger.log(logrus.WarnLevel, fmt.Sprintf(format, args...), e.fields)
}

// Error logs an error message with fields
func (e *AsyncLoggerEntry) Error(msg string) {
	e.logger.log(logrus.ErrorLevel, msg, e.fields)
}

// Errorf logs a formatted error message with fields
func (e *AsyncLoggerEntry) Errorf(format string, args ...interface{}) {
	e.logger.log(logrus.ErrorLevel, fmt.Sprintf(format, args...), e.fields)
}
