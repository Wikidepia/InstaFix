package handlers

import (
	"bytes"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kelindar/binary"
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

	inChan = make(chan remoteResult)

	ln, err := net.ListenTCP("tcp", listenAddr)
	if err != nil {
		return err
	}
	slog.Info("remote scraper is listening on", "address", ln.Addr())

	go func(ln *net.TCPListener, authCode []byte) {
		for {
			conn, err := ln.Accept()
			if err != nil {
				conn.Close()
				continue
			}

			// deadline for read 5s
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))

			authBytes := make([]byte, 8)
			n, err := conn.Read(authBytes)
			if err != nil || !bytes.Equal(authBytes[:n], authCode) {
				conn.Close()
				continue
			}
			conn.SetReadDeadline(time.Time{})

			go handleConnection(conn)
		}
	}(ln, authCode)
	return err
}

func handleConnection(conn net.Conn) {
	var wg sync.WaitGroup

	defer func() {
		conn.Close()
		wg.Done()
	}()
	wg.Add(1)

	for rm := range inChan {
		var err error
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			slog.Error("failed to set deadline", "err", err)
			rm.outChan <- err
			return
		}

		buf := []byte(rm.instaData.PostID)
		if _, err = conn.Write(buf); err != nil {
			slog.Error("failed to write to stream", "err", err)
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

		if err = binary.Unmarshal(outBuf[:n], rm.instaData); err != nil {
			slog.Error("failed to unmarshal data", "err", err)
			rm.outChan <- err
			continue
		}

		if rm.instaData.Username == "" {
			rm.outChan <- errors.New("remote scraper returns empty data")
			continue
		}
		rm.outChan <- nil
	}

	wg.Wait()
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
	case <-time.After(5 * time.Second):
		return errors.New("failed to get data from remote scraper")
	}
}

func GetRemoteSessCount() int {
	return int(sessCount.Load())
}
