package connectionmanager

import (
	"bytes"
	"context"
	"encoding/binary"
	nodestorage "fs/internal/storage/node_storage"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockConn struct {
	net.Conn
	// readBuf     bytes.Buffer
	writeBuf    bytes.Buffer
	closed      bool
	readErr     error
	setReadData [][]byte
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	if m.readErr != nil {
		return 0, m.readErr
	}
	if len(m.setReadData) == 0 {
		return 0, io.EOF
	}

	data := m.setReadData[0]
	n = copy(b, data)
	m.setReadData = m.setReadData[1:]
	return n, nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return m.writeBuf.Write(b)
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func TestClose(t *testing.T) {
	mockConn := &mockConn{}
	endpoint := nodestorage.Endpoint{NodeID: "test", Address: "localhost", Protocol: "tcp"}
	conn := NewTCPConnection(mockConn, endpoint)

	err := conn.Close()
	require.NoError(t, err)

	active := conn.IsActive()
	assert.False(t, active)

	closed := mockConn.closed
	assert.True(t, closed)

	_, ok := <-conn.messageChan
	assert.False(t, ok)
}

func TestStartReading_SizeReadError(t *testing.T) {
	mockConn := &mockConn{readErr: io.EOF}
	endpoint := nodestorage.Endpoint{NodeID: "test", Address: "localhost", Protocol: "tcp"}
	conn := NewTCPConnection(mockConn, endpoint)

	conn.StartReading(context.Background())
	time.Sleep(100 * time.Millisecond)

	assert.False(t, conn.IsActive(), "Connection should be closed on read error")
	assert.True(t, mockConn.closed, "Connection should by physically closed")
}

func TestStartReading_BodyReadError(t *testing.T) {
	mockConn := &mockConn{}
	endpoint := nodestorage.Endpoint{NodeID: "test", Address: "localhost", Protocol: "tcp"}
	conn := NewTCPConnection(mockConn, endpoint)

	size := make([]byte, 4)
	binary.BigEndian.PutUint32(size, 5)
	mockConn.setReadData = [][]byte{size}

	conn.StartReading(context.Background())
	time.Sleep(100 * time.Millisecond)
	assert.False(t, conn.IsActive(), "Connection should be closed on body read error")
}

func TestStartReading_InvalidProtoData(t *testing.T) {
	mockConn := &mockConn{}
	endpoint := nodestorage.Endpoint{NodeID: "test", Address: "localhost", Protocol: "tcp"}
	conn := NewTCPConnection(mockConn, endpoint)

	size := make([]byte, 4)
	binary.BigEndian.PutUint32(size, 4)
	mockConn.setReadData = [][]byte{
		size,
		[]byte{0xDE, 0xAD, 0xBE, 0xEF},
	}
	conn.StartReading(context.Background())
	time.Sleep(100 * time.Millisecond)

	assert.False(t, conn.IsActive(), "Connection should stay active on proto error")
	assert.Zero(t, len(conn.messageChan), "No messags should be in channel")
}
