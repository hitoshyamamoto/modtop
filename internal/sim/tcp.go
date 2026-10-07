package sim

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"
)

// TCPServer serves a Device over Modbus TCP.
type TCPServer struct {
	dev  *Device
	ln   net.Listener
	wg   sync.WaitGroup
	mu   sync.Mutex
	conn map[net.Conn]bool
}

// ListenTCP starts serving dev on addr (use "127.0.0.1:0" for an ephemeral port).
func ListenTCP(addr string, dev *Device) (*TCPServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &TCPServer{dev: dev, ln: ln, conn: map[net.Conn]bool{}}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

// Addr returns the listening address.
func (s *TCPServer) Addr() string { return s.ln.Addr().String() }

// Close stops the server and closes all connections.
func (s *TCPServer) Close() error {
	err := s.ln.Close()
	s.mu.Lock()
	for c := range s.conn {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return err
}

func (s *TCPServer) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conn[c] = true
		s.mu.Unlock()
		s.wg.Add(1)
		go s.serve(c)
	}
}

func (s *TCPServer) serve(c net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.conn, c)
		s.mu.Unlock()
		_ = c.Close()
	}()
	header := make([]byte, 7)
	for {
		if _, err := io.ReadFull(c, header); err != nil {
			return
		}
		length := int(binary.BigEndian.Uint16(header[4:]))
		if length < 2 || length > 254 {
			return
		}
		pdu := make([]byte, length-1)
		if _, err := io.ReadFull(c, pdu); err != nil {
			return
		}
		tid := binary.BigEndian.Uint16(header[0:])
		resp, delay, reply := s.dev.handle(header[6], pdu)
		faults := s.dev.takeFaults()
		if faults.CloseConn {
			return
		}
		if !reply {
			continue
		}
		time.Sleep(delay)
		if faults.StaleTID {
			if _, err := c.Write(mbap(tid-1, header[6], resp)); err != nil {
				return
			}
		}
		if _, err := c.Write(mbap(tid, header[6], resp)); err != nil {
			return
		}
	}
}

func mbap(tid uint16, unit byte, pdu []byte) []byte {
	f := make([]byte, 7, 7+len(pdu))
	binary.BigEndian.PutUint16(f[0:], tid)
	binary.BigEndian.PutUint16(f[4:], uint16(1+len(pdu)))
	f[6] = unit
	return append(f, pdu...)
}
