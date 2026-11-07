package commands

import (
	"encoding/json"
	"errors"

	"github.com/nirui/sshwifty/application/command"
)

type keyboardChallenge struct {
	Name        string   `json:"name"`
	Instruction string   `json:"instruction"`
	Questions   []string `json:"questions"`
	Echo        []bool   `json:"echo"`
}

func (d *sshClient) attachSession(ps *PersistentSession, attachment *SessionAttachment) bool {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if d.closing {
		return false
	}
	d.persistentSession, d.attachment = ps, attachment
	return true
}

func (d *sshClient) currentSession() (*PersistentSession, *SessionAttachment) {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	return d.persistentSession, d.attachment
}

// requestCredential retains the fork's existing marker and empty password/key
// request. Only Keyboard Interactive requests carry a challenge payload.
func (d *sshClient) requestCredential(payload []byte) ([]byte, error) {
	if len(payload)+d.w.HeaderSize() > command.StreamHeaderMaxLength {
		return nil, errors.New("authentication challenge is too large")
	}
	d.credentialMu.Lock()
	d.credentialPending = true
	d.credentialMu.Unlock()
	defer func() {
		d.credentialMu.Lock()
		d.credentialPending = false
		d.credentialMu.Unlock()
	}()
	buf := make([]byte, d.w.HeaderSize()+len(payload))
	copy(buf[d.w.HeaderSize():], payload)
	if err := d.w.SendManual(SSHServerConnectRequestCredential, buf); err != nil {
		return nil, err
	}
	select {
	case answer, ok := <-d.credentialReceive:
		if !ok {
			return nil, ErrSSHAuthCancelled
		}
		return answer, nil
	case <-d.baseCtx.Done():
		return nil, ErrSSHAuthCancelled
	}
}

func decodeKeyboardAnswers(data []byte, count int) ([]string, error) {
	if len(data) > sshCredentialMaxSize {
		return nil, ErrSSHCredentialDataTooLarge
	}
	var answers []string
	if err := json.Unmarshal(data, &answers); err != nil {
		return nil, errors.New("invalid authentication answers")
	}
	if answers == nil || len(answers) != count {
		return nil, errors.New("authentication answer count mismatch")
	}
	return answers, nil
}

func (d *sshClient) keyboardInteractive(user, instruction string, questions []string, echo []bool) ([]string, error) {
	if len(questions) != len(echo) {
		return nil, errors.New("invalid authentication challenge")
	}
	if questions == nil {
		questions = []string{}
	}
	if echo == nil {
		echo = []bool{}
	}
	payload, err := json.Marshal(keyboardChallenge{user, instruction, questions, echo})
	if err != nil {
		return nil, err
	}
	d.enableRemoteReadTimeoutRetry()
	defer d.disableRemoteReadTimeoutRetry()
	answer, err := d.requestCredential(payload)
	if err != nil {
		return nil, err
	}
	// Answers, including one-time codes, are never placed in reconnect storage.
	return decodeKeyboardAnswers(answer, len(questions))
}
