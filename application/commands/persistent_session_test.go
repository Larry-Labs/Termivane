package commands

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
)

func testPersistentSession(t *testing.T) *PersistentSession {
	t.Helper()
	ps := &PersistentSession{Output: NewRingBuffer(ringBufferSize)}
	t.Cleanup(ps.Close)
	return ps
}

func attachSession(t *testing.T, ps *PersistentSession) (*SessionAttachment, []byte) {
	t.Helper()
	a, snapshot, err := ps.Attach()
	if err != nil {
		t.Fatal(err)
	}
	return a, snapshot
}

func awaitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not finish")
	}
}

func TestPersistentOutputBackpressure(t *testing.T) {
	ps := testPersistentSession(t)
	a, _ := attachSession(t, ps)
	for i := 0; i < 256; i++ {
		ps.publish([]byte{byte(i)}, false)
	}
	done := make(chan struct{})
	go func() { ps.publish([]byte("tail"), false); close(done) }()
	select {
	case <-done:
		t.Fatal("full output queue dropped data instead of applying backpressure")
	case <-time.After(20 * time.Millisecond):
	}
	for i := 0; i < 256; i++ {
		data := <-a.Output
		if !bytes.Equal(data, []byte{0, byte(i)}) {
			t.Fatalf("output %d: %v", i, data)
		}
	}
	awaitDone(t, done)
	if data := <-a.Output; !bytes.Equal(data, append([]byte{0}, "tail"...)) {
		t.Fatalf("missing final output: %v", data)
	}
}

func TestPersistentDetachUnblocksOutput(t *testing.T) {
	for _, closeSession := range []bool{false, true} {
		ps := testPersistentSession(t)
		a, _ := attachSession(t, ps)
		for i := 0; i < 256; i++ {
			ps.publish([]byte("x"), false)
		}
		done := make(chan struct{})
		go func() { ps.publish([]byte("tail"), false); close(done) }()
		if closeSession {
			ps.Close()
		} else {
			ps.Detach(a)
		}
		awaitDone(t, done)
		drained := make(chan struct{})
		go func() {
			for range a.Output {
			}
			close(drained)
		}()
		awaitDone(t, drained)
	}
}

func TestPersistentOldDetachDoesNotDisconnectReplacement(t *testing.T) {
	ps := testPersistentSession(t)
	old, _ := attachSession(t, ps)
	ps.publish([]byte("before"), false)
	next, snapshot := attachSession(t, ps)
	if string(snapshot) != "before" {
		t.Fatalf("snapshot: %q", snapshot)
	}
	ps.Detach(old)
	if !ps.IsAttached() {
		t.Fatal("old browser detached the new browser")
	}
	ps.publish([]byte("after"), true)
	select {
	case out := <-next.Output:
		if string(out[1:]) != "after" || out[0] != 1 {
			t.Fatalf("output: %v", out)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement did not receive output")
	}
}

func TestPersistentSnapshotAndAttachHaveNoGap(t *testing.T) {
	ps := testPersistentSession(t)
	// Keep the whole test stream inside the production history bound.
	want := bytes.Repeat([]byte("0123456789"), 200)
	done := make(chan struct{})
	start := make(chan struct{})
	go func() {
		<-start
		for _, b := range want {
			ps.publish([]byte{b}, false)
		}
		close(done)
	}()
	close(start)
	a, snapshot := attachSession(t, ps)
	got := append([]byte(nil), snapshot...)
	go func() { <-done; ps.Detach(a) }()
	for data := range a.Output {
		got = append(got, data[1:]...)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("replay + live output differs: got %d bytes, want %d", len(got), len(want))
	}
}

func TestPersistentConcurrentAttachmentChanges(t *testing.T) {
	ps := testPersistentSession(t)
	var readers sync.WaitGroup
	published := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			ps.publish([]byte("output"), i%2 == 0)
		}
		close(published)
	}()
	var old *SessionAttachment
	for i := 0; i < 100; i++ {
		a, _ := attachSession(t, ps)
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range a.Output {
			}
		}()
		ps.Detach(old)
		old = a
	}
	awaitDone(t, published)
	ps.Detach(old)
	done := make(chan struct{})
	go func() { readers.Wait(); close(done) }()
	awaitDone(t, done)
}

func TestPersistentDrainsBothStreamsBeforeClosing(t *testing.T) {
	ps := testPersistentSession(t)
	stderr, stderrWriter := io.Pipe()
	t.Cleanup(func() { stderr.Close(); stderrWriter.Close() })
	ps.Stdout = bytes.NewReader([]byte("stdout"))
	ps.Stderr = stderr
	a, _ := attachSession(t, ps)
	ps.Start()
	first := <-a.Output
	if string(first[1:]) != "stdout" {
		t.Fatalf("stdout: %q", first)
	}
	// stdout EOF must not prevent stderr from publishing its final bytes.
	if ps.IsClosed() {
		t.Fatal("stdout EOF closed the whole session")
	}
	done := make(chan struct{})
	go func() {
		_, _ = stderrWriter.Write([]byte("stderr tail"))
		stderrWriter.Close()
		close(done)
	}()
	var tail []byte
	for out := range a.Output {
		tail = append(tail, out[1:]...)
	}
	awaitDone(t, done)
	if string(tail) != "stderr tail" {
		t.Fatalf("stderr was truncated: %q", tail)
	}
	if !ps.IsClosed() {
		t.Fatal("both EOFs must finish the session")
	}
}

func TestPersistentAttachAfterCloseFails(t *testing.T) {
	ps := testPersistentSession(t)
	ps.Close()
	if _, _, err := ps.Attach(); err != io.ErrClosedPipe {
		t.Fatalf("attach closed session: %v", err)
	}
}

func TestRingBufferRetainsLatestOutput(t *testing.T) {
	r := NewRingBuffer(5)
	r.Write([]byte("abc"))
	r.Write([]byte("defgh"))
	if got := string(r.Snapshot()); got != "defgh" {
		t.Fatalf("ring snapshot: %q", got)
	}
}
