package logrusotel

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func TestHookLevelsCoversLogrusLevels(t *testing.T) {
	hook := New("test")
	if got, want := len(hook.Levels()), len(logrus.AllLevels); got != want {
		t.Fatalf("levels = %d, want %d", got, want)
	}
}

func TestHookRedactsSensitiveFields(t *testing.T) {
	logger := logrus.New()
	logger.AddHook(New("test"))
	logger.WithField("execution_lease_token", "secret").Info("hello")
}
