package retry

import (
	"context"
	"io"
	"testing"
	"time"

	"net"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttempts(t *testing.T) {
	t.Run("Respects count and sleeps between attempts", func(t *testing.T) {
		count := 0
		start := time.Now()

		Attempts(5, time.Millisecond, func() error {
			count++
			return errors.Errorf("asdfasdf")
		})

		assert.Equal(t, 5, count)
		assert.WithinDuration(t, start.Add(time.Millisecond*5), time.Now(), time.Millisecond*1)
	})

	t.Run("returns as soon as error is nil", func(t *testing.T) {
		start := time.Now()
		Attempts(100, time.Minute, func() error {
			return nil
		})
		assert.WithinDuration(t, time.Now(), start, time.Millisecond)
	})
}

func TestTimeout(t *testing.T) {
	t.Run("Respects timeout and sleeps between attempts", func(t *testing.T) {
		count := 0
		start := time.Now()

		// The timing here is a little sketchy.
		Timeout((time.Millisecond * 5), time.Millisecond, func() error {
			count++
			return errors.Errorf("asdfasdf")
		})

		assert.Equal(t, 5, count)
		assert.WithinDuration(t, start.Add(time.Millisecond*5), time.Now(), time.Millisecond*1)
	})

	t.Run("returns as soon as error is nil", func(t *testing.T) {
		start := time.Now()
		Timeout((time.Hour), time.Minute, func() error {
			return nil
		})
		assert.WithinDuration(t, time.Now(), start, time.Millisecond)
	})
}

func TestBackoff(t *testing.T) {
	t.Parallel()

	t.Run("return when nil", func(t *testing.T) {
		var count int
		err := Backoff(time.Minute, time.Second, time.Millisecond, func() error {
			count++
			if count == 10 {
				return nil
			}
			return io.EOF
		})
		assert.Equal(t, 10, count)
		assert.NoError(t, err)
	})

	t.Run("don't exceed deadline dramatically", func(t *testing.T) {
		start := time.Now()
		Backoff(time.Second, time.Millisecond*5, time.Millisecond, func() error {
			time.Sleep(time.Millisecond * 5)
			return io.EOF
		})
		assert.WithinDuration(t, start.Add(time.Second), time.Now(), time.Millisecond*10)
	})

	t.Run("Run until nil error", func(t *testing.T) {
		start := time.Now()
		err := Backoff(0, time.Second*5, time.Millisecond*200, func() error {
			if time.Now().Sub(start) > time.Second {
				return nil
			}
			return io.EOF
		})
		require.NoError(t, err)
	})
}

func TestBackoffContext(t *testing.T) {
	t.Run("return when nil", func(t *testing.T) {
		ctx, _ := context.WithTimeout(context.Background(), time.Minute)
		var count int
		err := BackoffContext(ctx, time.Second, time.Millisecond, func() error {
			count++
			if count == 10 {
				return nil
			}
			return io.EOF
		})
		assert.Equal(t, 10, count)
		assert.NoError(t, err)
	})

	t.Run("respect context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(time.Millisecond*100, cancel)
		start := time.Now()
		BackoffContext(ctx, time.Millisecond*5, time.Millisecond, func() error {
			return io.EOF
		})
		assert.WithinDuration(t, start.Add(time.Millisecond*100), time.Now(), time.Millisecond*10)
	})
}

type testListener struct {
	acceptFn func() (net.Conn, error)
}

func newTestListener(acceptFn func() (net.Conn, error)) net.Listener {
	return &Listener{
		LogTmpErr: func(err error) {},
		Listener: &testListener{
			acceptFn: acceptFn,
		},
	}
}

func (l *testListener) Accept() (net.Conn, error) {
	return l.acceptFn()
}

func (l *testListener) Close() error {
	panic("do not call")
}

func (l *testListener) Addr() net.Addr {
	panic("do not call")
}

type testNetError struct {
	temporary bool
}

func (e *testNetError) Error() string {
	return "test net error"
}

func (e *testNetError) Temporary() bool {
	return e.temporary
}

func (e *testNetError) Timeout() bool {
	panic("do not call")
}

func TestListener(t *testing.T) {
	t.Parallel()
	t.Run("general error", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("general error")
		acceptFn := func() (net.Conn, error) {
			return nil, expectedErr
		}

		_, err := newTestListener(acceptFn).Accept()
		require.Equal(t, expectedErr, err)
	})
	t.Run("success", func(t *testing.T) {
		t.Parallel()

		acceptFn := func() (net.Conn, error) {
			return nil, nil
		}

		_, err := newTestListener(acceptFn).Accept()
		require.Nil(t, err)
	})
	t.Run("non temp net error", func(t *testing.T) {
		t.Parallel()

		expectedErr := &testNetError{false}
		acceptFn := func() (net.Conn, error) {
			return nil, expectedErr
		}

		_, err := newTestListener(acceptFn).Accept()
		require.Equal(t, expectedErr, err)
	})
	t.Run("3x temp net error", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		acceptFn := func() (net.Conn, error) {
			callCount++
			switch callCount {
			case 1:
				return nil, &testNetError{true}
			case 2:
				return nil, &testNetError{true}
			case 3:
				return nil, nil
			default:
				t.Fatal("test listener called too many times; callCount: %v", callCount)
				panic("unreachable")
			}
		}

		_, err := newTestListener(acceptFn).Accept()
		require.Nil(t, err)
		require.Equal(t, callCount, 3)
	})
}
