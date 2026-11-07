package commands

import (
	"strings"
	"testing"
)

func TestDecodeKeyboardAnswers(t *testing.T) {
	for _, tc := range []struct {
		data  string
		count int
		valid bool
	}{
		{`["测试","123456"]`, 2, true},
		{`[]`, 0, true},
		{`null`, 0, false},
		{`["one"]`, 2, false},
		{`[123]`, 1, false},
		{`not json`, 0, false},
		{`["` + strings.Repeat("中", 1400) + `"]`, 1, false},
	} {
		_, err := decodeKeyboardAnswers([]byte(tc.data), tc.count)
		if (err == nil) != tc.valid {
			t.Errorf("valid=%v, got error %v", tc.valid, err)
		}
	}
}

func TestClosedClientCannotAttachSession(t *testing.T) {
	d := &sshClient{closing: true}
	if d.attachSession(&PersistentSession{}, nil) {
		t.Fatal("closed client acquired session")
	}
}
