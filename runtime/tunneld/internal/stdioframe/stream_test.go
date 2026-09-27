package stdioframe

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
)

type oneByteWriter struct{ dst *bytes.Buffer }

func (w oneByteWriter) Write(data []byte) (int, error) {
	return w.dst.Write(data[:1])
}

func TestRoundTripThroughFragmentedIO(t *testing.T) {
	var raw bytes.Buffer
	frame := &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamData{
		StreamData: &tunnelv1.StreamData{StreamID: 7, Data: []byte("payload")},
	}}
	if err := New(nil, oneByteWriter{dst: &raw}).Send(frame); err != nil {
		t.Fatal(err)
	}
	reader := &oneByteReader{src: bytes.NewReader(raw.Bytes())}
	got, err := New(reader, nil).Recv()
	if err != nil {
		t.Fatal(err)
	}
	if got.GetStreamData().GetStreamID() != 7 || string(got.GetStreamData().GetData()) != "payload" {
		t.Fatalf("round trip frame = %v", got)
	}
	if _, err := New(reader, nil).Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("end of frame stream = %v, want EOF", err)
	}
}

type oneByteReader struct{ src *bytes.Reader }

func (r *oneByteReader) Read(data []byte) (int, error) {
	return r.src.Read(data[:1])
}

func TestConcurrentSendKeepsFrameBoundaries(t *testing.T) {
	var raw bytes.Buffer
	stream := New(nil, &raw)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := stream.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamOpen{
				StreamOpen: &tunnelv1.StreamOpen{StreamID: 1},
			}}); err != nil {
				t.Errorf("send: %v", err)
			}
		}()
	}
	wg.Wait()
	reader := New(&raw, nil)
	for i := 0; i < 32; i++ {
		frame, err := reader.Recv()
		if err != nil || frame.GetStreamOpen().GetStreamID() != 1 {
			t.Fatalf("frame %d = %v, %v", i, frame, err)
		}
	}
}

func TestReceiveRejectsInvalidOrTruncatedFrames(t *testing.T) {
	var tooLarge [4]byte
	binary.BigEndian.PutUint32(tooLarge[:], MaxFrameBytes+1)
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"zero length", []byte{0, 0, 0, 0}, "invalid tunnel frame length"},
		{"oversize", tooLarge[:], "invalid tunnel frame length"},
		{"partial header", []byte{0, 0}, "unexpected EOF"},
		{"partial payload", []byte{0, 0, 0, 3, 1}, "unexpected EOF"},
		{"malformed protobuf", []byte{0, 0, 0, 1, 0xff}, "decode tunnel frame"},
		{"empty payload", []byte{0, 0, 0, 1, 0}, "decode tunnel frame"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(bytes.NewReader(tc.data), nil).Recv()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Recv() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSendRejectsUnboundedData(t *testing.T) {
	frame := &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamData{
		StreamData: &tunnelv1.StreamData{Data: make([]byte, MaxDataBytes+1)},
	}}
	if err := New(nil, io.Discard).Send(frame); err == nil {
		t.Fatal("oversized tunnel frame was accepted")
	}
}

func TestSendRejectsShortWrite(t *testing.T) {
	frame := &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamOpen{StreamOpen: &tunnelv1.StreamOpen{StreamID: 1}}}
	if err := New(nil, shortWriter{}).Send(frame); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Send() error = %v, want short write", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }
