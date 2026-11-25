package serialx

import (
	"errors"
	"time"

	"go.bug.st/serial"
)

var ErrWriteTimeout = errors.New("serial write timed out")

func Write(p serial.Port, b []byte, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {

		for len(b) > 0 {
			n, err := p.Write(b)
			if err != nil {
				done <- err
				return
			}
			if n <= 0 {
				done <- errors.New("serial write accepted no data")
				return
			}
			b = b[n:]
		}
		done <- nil
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		cancelIO(p)
		_ = p.ResetOutputBuffer()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		return ErrWriteTimeout
	}
}
