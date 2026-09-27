package stdioframe

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/protobuf/proto"
)

// ReadyLine is the fixed startup acknowledgement before the binary stream.
// The reader must consume exactly len(ReadyLine) bytes before calling Recv.
const ReadyLine = "ready\n"

const (
	MaxDataBytes  = 1 << 20
	MaxFrameBytes = MaxDataBytes + 4096
)

// Stream carries TunnelFrame messages over runsc exec's inherited stdio. It
// does not carry relay credentials or grant the guest access to a host socket.
// One goroutine may call Recv while any number of goroutines call Send.
type Stream struct {
	reader io.Reader
	writer io.Writer
	write  sync.Mutex
}

func New(reader io.Reader, writer io.Writer) *Stream {
	return &Stream{reader: reader, writer: writer}
}

func (s *Stream) Send(frame *tunnelv1.TunnelFrame) error {
	if frame == nil || frame.GetPayload() == nil {
		return fmt.Errorf("tunnel frame payload is required")
	}
	if data := frame.GetStreamData(); data != nil && len(data.GetData()) > MaxDataBytes {
		return fmt.Errorf("tunnel frame data exceeds %d bytes", MaxDataBytes)
	}
	if size := proto.Size(frame); size == 0 || size > MaxFrameBytes {
		return fmt.Errorf("encoded tunnel frame exceeds %d bytes", MaxFrameBytes)
	}
	encoded, err := proto.Marshal(frame)
	if err != nil {
		return fmt.Errorf("encode tunnel frame: %w", err)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(encoded)))
	s.write.Lock()
	defer s.write.Unlock()
	if err := writeAll(s.writer, header[:]); err != nil {
		return err
	}
	return writeAll(s.writer, encoded)
}

func (s *Stream) Recv() (*tunnelv1.TunnelFrame, error) {
	var header [4]byte
	if _, err := io.ReadFull(s.reader, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > MaxFrameBytes {
		return nil, fmt.Errorf("invalid tunnel frame length %d", length)
	}
	encoded := make([]byte, length)
	if _, err := io.ReadFull(s.reader, encoded); err != nil {
		return nil, err
	}
	frame := &tunnelv1.TunnelFrame{}
	if err := proto.Unmarshal(encoded, frame); err != nil {
		return nil, fmt.Errorf("decode tunnel frame: %w", err)
	}
	if frame.GetPayload() == nil {
		return nil, fmt.Errorf("tunnel frame payload is required")
	}
	if data := frame.GetStreamData(); data != nil && len(data.GetData()) > MaxDataBytes {
		return nil, fmt.Errorf("tunnel frame data exceeds %d bytes", MaxDataBytes)
	}
	return frame, nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
