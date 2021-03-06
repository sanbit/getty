/******************************************************
# DESC       : tcp/websocket connection
# MAINTAINER : Alex Stocks
# LICENCE    : Apache License 2.0
# EMAIL      : alexstocks@foxmail.com
# MOD        : 2016-08-17 11:21
# FILE       : conn.go
******************************************************/

package getty

import (
	// "errors"
	"net"
	"sync/atomic"
	"time"
)

import (
	log "github.com/AlexStocks/log4go"
	"github.com/gorilla/websocket"
)

var (
	launchTime time.Time = time.Now()

// ErrInvalidConnection = errors.New("connection has been closed.")
)

/////////////////////////////////////////
// connection interfacke
/////////////////////////////////////////

type iConn interface {
	incReadPkgCount()
	incWritePkgCount()
	updateActive()
	getActive() time.Time
	readDeadline() time.Duration
	setReadDeadline(time.Duration)
	writeDeadline() time.Duration
	setWriteDeadline(time.Duration)
	write(p []byte) error
	// don't distinguish between tcp connection and websocket connection. Because
	// gorilla/websocket/conn.go:(Conn)Close also invoke net.Conn.Close
	close(int)
}

/////////////////////////////////////////
// getty connection
/////////////////////////////////////////

var (
	connID uint32
)

type gettyConn struct {
	ID            uint32
	padding       uint32        // last active, in milliseconds
	readCount     uint32        // read() count
	writeCount    uint32        // write() count
	readPkgCount  uint32        // send pkg count
	writePkgCount uint32        // recv pkg count
	active        int64         // active
	rDeadline     time.Duration // network current limiting
	wDeadline     time.Duration
	local         string // local address
	peer          string // peer address
}

func (this *gettyConn) incReadPkgCount() {
	atomic.AddUint32(&this.readPkgCount, 1)
}

func (this *gettyConn) incWritePkgCount() {
	atomic.AddUint32(&this.writePkgCount, 1)
}

func (this *gettyConn) updateActive() {
	atomic.StoreInt64(&(this.active), int64(time.Since(launchTime)))
}

func (this *gettyConn) getActive() time.Time {
	return launchTime.Add(time.Duration(atomic.LoadInt64(&(this.active))))
}

func (this *gettyConn) write([]byte) error {
	return nil
}

func (this *gettyConn) close(int) {}

func (this gettyConn) readDeadline() time.Duration {
	return this.rDeadline
}

func (this *gettyConn) setReadDeadline(rDeadline time.Duration) {
	if rDeadline < 1 {
		panic("@rDeadline < 1")
	}

	this.rDeadline = rDeadline
	if this.wDeadline == 0 {
		this.wDeadline = rDeadline
	}
}

func (this gettyConn) writeDeadline() time.Duration {
	return this.wDeadline
}

func (this *gettyConn) setWriteDeadline(wDeadline time.Duration) {
	if wDeadline < 1 {
		panic("@wDeadline < 1")
	}

	this.wDeadline = wDeadline
}

/////////////////////////////////////////
// getty tcp connection
/////////////////////////////////////////

type gettyTCPConn struct {
	gettyConn
	conn net.Conn
}

// create gettyTCPConn
func newGettyTCPConn(conn net.Conn) *gettyTCPConn {
	if conn == nil {
		panic("newGettyTCPConn(conn):@conn is nil")
	}
	var localAddr, peerAddr string
	//  check conn.LocalAddr or conn.RemoetAddr is nil to defeat panic on 2016/09/27
	if conn.LocalAddr() != nil {
		localAddr = conn.LocalAddr().String()
	}
	if conn.RemoteAddr() != nil {
		peerAddr = conn.RemoteAddr().String()
	}

	return &gettyTCPConn{
		conn: conn,
		gettyConn: gettyConn{
			ID:    atomic.AddUint32(&connID, 1),
			local: localAddr,
			peer:  peerAddr,
		},
	}
}

// tcp connection read
func (this *gettyTCPConn) read(p []byte) (int, error) {
	// if this.conn == nil {
	//	return 0, ErrInvalidConnection
	// }

	// atomic.AddUint32(&this.readCount, 1)
	l, e := this.conn.Read(p)
	atomic.AddUint32(&this.readCount, uint32(l))
	return l, e
}

// tcp connection write
func (this *gettyTCPConn) write(p []byte) error {
	// if this.conn == nil {
	//	return 0, ErrInvalidConnection
	// }

	// atomic.AddUint32(&this.writeCount, 1)
	atomic.AddUint32(&this.writeCount, (uint32)(len(p)))
	_, err := this.conn.Write(p)
	return err
}

// close tcp connection
func (this *gettyTCPConn) close(waitSec int) {
	// if tcpConn, ok := this.conn.(*net.TCPConn); ok {
	// tcpConn.SetLinger(0)
	// }

	if this.conn != nil {
		this.conn.(*net.TCPConn).SetLinger(waitSec)
		this.conn.Close()
		this.conn = nil
	}
}

/////////////////////////////////////////
// getty websocket connection
/////////////////////////////////////////

type gettyWSConn struct {
	gettyConn
	conn *websocket.Conn
}

// create websocket connection
func newGettyWSConn(conn *websocket.Conn) *gettyWSConn {
	if conn == nil {
		panic("newGettyWSConn(conn):@conn is nil")
	}
	var localAddr, peerAddr string
	//  check conn.LocalAddr or conn.RemoetAddr is nil to defeat panic on 2016/09/27
	if conn.LocalAddr() != nil {
		localAddr = conn.LocalAddr().String()
	}
	if conn.RemoteAddr() != nil {
		peerAddr = conn.RemoteAddr().String()
	}

	gettyWSConn := &gettyWSConn{
		conn: conn,
		gettyConn: gettyConn{
			ID:    atomic.AddUint32(&connID, 1),
			local: localAddr,
			peer:  peerAddr,
		},
	}
	conn.SetPingHandler(gettyWSConn.handlePing)
	conn.SetPongHandler(gettyWSConn.handlePong)

	return gettyWSConn
}

func (this *gettyWSConn) handlePing(message string) error {
	err := this.conn.WriteMessage(websocket.PongMessage, []byte(message))
	if err == websocket.ErrCloseSent {
		err = nil
	} else if e, ok := err.(net.Error); ok && e.Temporary() {
		err = nil
	}
	if err == nil {
		this.updateActive()
	}

	return err
}

func (this *gettyWSConn) handlePong(string) error {
	this.updateActive()
	return nil
}

// websocket connection read
func (this *gettyWSConn) read() ([]byte, error) {
	// this.conn.SetReadDeadline(time.Now().Add(this.rDeadline))
	_, b, e := this.conn.ReadMessage() // the first return value is message type.
	if e == nil {
		// atomic.AddUint32(&this.readCount, (uint32)(l))
		atomic.AddUint32(&this.readPkgCount, 1)
	} else {
		if websocket.IsUnexpectedCloseError(e, websocket.CloseGoingAway) {
			log.Warn("websocket unexpected close error: %v", e)
		}
	}

	return b, e
}

// websocket connection write
func (this *gettyWSConn) write(p []byte) error {
	// atomic.AddUint32(&this.writeCount, 1)
	atomic.AddUint32(&this.writeCount, (uint32)(len(p)))
	// this.conn.SetWriteDeadline(time.Now().Add(this.wDeadline))
	return this.conn.WriteMessage(websocket.BinaryMessage, p)
}

func (this *gettyWSConn) writePing() error {
	return this.conn.WriteMessage(websocket.PingMessage, []byte{})
}

// close websocket connection
func (this *gettyWSConn) close(waitSec int) {
	this.conn.WriteMessage(websocket.CloseMessage, []byte("bye-bye!!!"))
	this.conn.UnderlyingConn().(*net.TCPConn).SetLinger(waitSec)
	this.conn.Close()
}
