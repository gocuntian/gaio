package gaio

import (
	"bytes"
	"crypto/rand"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"testing"
)

func init() {

	go http.ListenAndServe(":6060", nil)
}

func echoServer(t testing.TB) net.Listener {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}

	w, err := CreateWatcher(4096)
	if err != nil {
		t.Fatal(err)
	}

	chRx := make(chan OpResult)
	chTx := make(chan OpResult)
	// ping-pong scheme echo server
	go func() {
		for {
			select {
			case res := <-chRx:
				if res.Err != nil {
					log.Println("read error:", res.Err, res.Size)
					w.StopWatch(res.Fd)
					continue
				}

				if res.Size == 0 {
					log.Println("client closed")
					w.StopWatch(res.Fd)
					continue
				}

				// write the data, we won't start to read again until write completes.
				buf := make([]byte, res.Size)
				copy(buf, res.Buffer[:res.Size])
				w.Write(res.Fd, buf, chTx)
			case res := <-chTx:
				if res.Err != nil {
					log.Println("write error:", res.Err, res.Size)
					w.StopWatch(res.Fd)
				}
				// write complete, start read again
				w.Read(res.Fd, chRx)
			}
		}
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				log.Println(err)
				return
			}

			fd, err := w.Watch(conn)
			if err != nil {
				log.Println(err)
				return
			}

			log.Println("watching", conn.RemoteAddr(), "fd:", fd)

			// kick off
			err = w.Read(fd, chRx)
			if err != nil {
				log.Println(err)
				return
			}
		}
	}()
	return ln
}

func TestEchoTiny(t *testing.T) {
	ln := echoServer(t)
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	tx := []byte("hello world")
	rx := make([]byte, len(tx))

	_, err = conn.Write(tx)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("tx:", string(tx))
	_, err = conn.Read(rx)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("rx:", string(tx))
	conn.Close()
}

func TestEchoHuge(t *testing.T) {
	ln := echoServer(t)
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	tx := make([]byte, 100*1024*1024)
	n, err := io.ReadFull(rand.Reader, tx)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		n, err := conn.Write(tx)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("ping size", n)
	}()

	rx := make([]byte, len(tx))
	n, err = io.ReadFull(conn, rx)
	if err != nil {
		t.Fatal(err, n)
	}
	t.Log("pong size:", n)

	if bytes.Compare(tx, rx) != 0 {
		t.Fatal("incorrect receiving")
	}
	conn.Close()
}

func BenchmarkEcho(b *testing.B) {
	ln := echoServer(b)

	addr, _ := net.ResolveTCPAddr("tcp", ln.Addr().String())
	tx := make([]byte, 1024*1024)
	_, err := io.ReadFull(rand.Reader, tx)
	if err != nil {
		b.Fatal(err)
	}

	rx := make([]byte, 65536)

	conn, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		b.Fatal(err)
		return
	}

	b.Log("sending", len(tx), "bytes for", b.N, "times")
	b.ReportAllocs()
	b.SetBytes(int64(len(tx)))
	b.ResetTimer()
	go func() {
		for i := 0; i < b.N; i++ {
			_, err := conn.Write(tx)
			if err != nil {
				b.Fatal(err)
			}
		}
	}()

	count := 0
	for {
		n, err := conn.Read(rx)
		if err != nil {
			b.Fatal(err)
		}
		count += n
		if count == len(tx)*b.N {
			break
		}
	}
	//		log.Println(i, b.N)
	conn.Close()
}
