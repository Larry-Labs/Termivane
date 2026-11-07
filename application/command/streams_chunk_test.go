package command

import (
	"bytes"
	"strconv"
	"sync"
	"testing"
)

func TestResponderSegmentsWithoutTruncation(t *testing.T) {
	for _, size := range []int{4, 64, 4096, StreamHeaderMaxLength + 20} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			var wire bytes.Buffer
			w := StreamResponder{w: streamHandlerSender{handlerSender: &handlerSender{writer: &wire, lock: &sync.Mutex{}}}}
			input := bytes.Repeat([]byte("terminal output"), 2000)
			if err := w.Send(1, input, make([]byte, size)); err != nil {
				t.Fatal(err)
			}
			var got []byte
			for wire.Len() > 0 {
				header := wire.Next(3)
				if len(header) != 3 {
					t.Fatal("incomplete header")
				}
				h := StreamHeader{header[1], header[2]}
				if h.Marker() != 1 {
					t.Fatal("incorrect marker")
				}
				chunk := wire.Next(int(h.Length()))
				if len(chunk) != int(h.Length()) {
					t.Fatal("header length exceeds payload")
				}
				got = append(got, chunk...)
			}
			if !bytes.Equal(got, input) {
				t.Fatalf("output truncated: got %d, want %d", len(got), len(input))
			}
		})
	}
}
