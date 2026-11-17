package syncer

import (
	"errors"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// no test locks skills in this machine's Hermes
	hermesBin = func() (string, error) { return "", errors.New("not found") }
	os.Exit(m.Run())
}
