package asynclog

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestAsyncLogger(t *testing.T) {
	logger := logrus.New()
	asyncLogger := NewAsyncLogger(logger, 100)
	defer asyncLogger.Close()

	// Test basic logging
	asyncLogger.Info("test info message")
	asyncLogger.Debugf("test debug message: %s", "formatted")
	asyncLogger.Warn("test warning")
	asyncLogger.Error("test error")

	// Test with fields
	asyncLogger.WithFields(logrus.Fields{
		"key1": "value1",
		"key2": 123,
	}).Info("test with fields")

	// Give time for async processing
	time.Sleep(100 * time.Millisecond)
}

func TestAsyncLoggerBufferFull(t *testing.T) {
	logger := logrus.New()
	asyncLogger := NewAsyncLogger(logger, 10) // Small buffer
	defer asyncLogger.Close()

	// Fill the buffer and overflow
	for i := 0; i < 100; i++ {
		asyncLogger.Infof("message %d", i)
	}

	time.Sleep(100 * time.Millisecond)

	dropped := asyncLogger.GetDroppedCount()
	if dropped == 0 {
		t.Logf("No logs were dropped (buffer processed fast enough)")
	} else {
		t.Logf("Dropped %d log entries as expected", dropped)
	}
}

func BenchmarkAsyncLogger(b *testing.B) {
	logger := logrus.New()
	asyncLogger := NewAsyncLogger(logger, 10000)
	defer asyncLogger.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		asyncLogger.Infof("benchmark message %d", i)
	}
}

func BenchmarkSyncLogger(b *testing.B) {
	logger := logrus.New()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Infof("benchmark message %d", i)
	}
}
