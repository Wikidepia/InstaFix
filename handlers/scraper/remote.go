package handlers

import (
	"bytes"
	"encoding/gob"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"time"
)

type remoteResult struct {
	instaData *InstaData
	outChan   chan error
}

var sessCount atomic.Int32
var inChan chan remoteResult

func InitRemoteScraper(listenAddr *net.TCPAddr, authCode []byte) error {
	if len(authCode) > 8 {
		return errors.New("auth code max length is 8 bytes")
	}

	inChan = make(chan remoteResult, 1000)

	ln, err := net.ListenTCP("tcp", listenAddr)
	if err != nil {
		return err
	}
	slog.Info("remote scraper is listening on", "address", ln.Addr())

	go func(ln *net.TCPListener, authCode []byte) {
		for {
			conn, err := ln.Accept()
			if err != nil {
				continue
			}

			go handleConnection(conn)
		}
	}(ln, authCode)
	return err
}

func handleConnection(conn net.Conn) {
	defer func() {
		sessCount.Add(-1)
		conn.Close()
	}()
	sessCount.Add(1)

	for {
		select {
		case rm := <-inChan:
			if err := conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
				slog.Error("failed to set deadline", "err", err)
				rm.outChan <- err
				return
			}

			buf := []byte(rm.instaData.PostID)
			_, err := conn.Write(buf)
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
					rm.outChan <- err
					continue
				} else if err != io.EOF {
					rm.outChan <- err
					slog.Error("write error", "err", err)
					return
				}
			}

			if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
				slog.Error("failed to set deadline", "err", err)
				rm.outChan <- err
				return
			}

			outBuf := make([]byte, 1024*1024)
			n, err := conn.Read(outBuf)
			if err != nil {
				slog.Error("failed to read from stream", "err", err)
				rm.outChan <- err
				return
			}
			if n == 1 {
				rm.outChan <- errors.New("remote scraper returns empty data")
				continue
			}

			var network bytes.Buffer
			dec := gob.NewDecoder(&network)
			network.Write(outBuf[:(n - 8)])
			err = dec.Decode(rm.instaData)
			if err != nil {
				slog.Error("failed to decode data", "err", err)
				rm.outChan <- err
				continue
			}

			if rm.instaData.Username == "" {
				rm.outChan <- errors.New("remote scraper returns empty data")
				continue
			}
			rm.outChan <- nil
		}
	}
}

func ScrapeRemote(i *InstaData) error {
	if sessCount.Load() == 0 {
		return errors.New("remote scraper is not running")
	}

	remoteRes := remoteResult{
		instaData: i,
		outChan:   make(chan error),
	}

	select {
	case inChan <- remoteRes:
	case <-time.After(time.Second):
		return errors.New("no remote scraper is ready")
	}

	select {
	case err := <-remoteRes.outChan:
		return err
	}
}

func GetRemoteSessCount() int {
	return int(sessCount.Load())
}
