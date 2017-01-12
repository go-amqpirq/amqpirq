package amqpirq

import (
	"crypto/tls"
	"github.com/streadway/amqp"
	"time"
)

const (
	defaultMaxAttempts = -1
	defaultDelay       = uint(30)
)

// MessageListener is enabling configuring channels and queues to implement
// client code depending on amqp.Connection to the broker. The Listen is
// invoked in a goroutine and the channel is closed when either a connection
// to the broker is lost or the amqpirq.Connection is closed.
type MessageListener interface {
	Listen(*amqp.Connection, <-chan struct{})
}

// Connection facilitates interruptible connectivity to RabbitMQ broker,
// re-attempting connects until max attempts (default unlimited) with
// configured delay (default 30 secods)
type Connection struct {
	// MaxAttempts is after how many unsuccessful attempts an error
	// is returned
	MaxAttempts int
	// Delay is number of seconds to wait before re-attempting connection
	Delay uint
	ch    chan *struct{}
	dial  func() (*amqp.Connection, error)
}

// Dial is a wrapper around amqp.Dial that accepts a string in the AMQP URI
// format and returns a new Connection over TCP using PlainAuth.
func Dial(url string) (*Connection, error) {
	return dialFunc(func() (*amqp.Connection, error) { return amqp.Dial(url) })
}

// DialTLS is a wrapper around amqp.DialTLS that accepts a string in the
// AMQP URI format and returns a new Connection over TCP using PlainAuth.
func DialTLS(url string, amqps *tls.Config) (*Connection, error) {
	return dialFunc(func() (*amqp.Connection, error) { return amqp.DialTLS(url, amqps) })
}

// DialConfig is a wrapper around amqp.DialConfig that accepts a string in the
// AMQP URI format and a configuration for the transport and connection setup,
// returning a new Connection.
func DialConfig(url string, config amqp.Config) (*Connection, error) {
	return dialFunc(func() (*amqp.Connection, error) { return amqp.DialConfig(url, config) })
}

func dialFunc(dial func() (*amqp.Connection, error)) (*Connection, error) {

	return &Connection{
		MaxAttempts: defaultMaxAttempts,
		Delay:       defaultDelay,
		dial:        dial,
		ch:          make(chan *struct{}, 64),
	}, nil

}

func (c *Connection) Close() {
	select {
	case c.ch <- new(struct{}):
	// ok
	default:
	}
}

func (c *Connection) Listen(listener MessageListener) (err error) {
	attemptCount := 0
	for {
		attemptCount++
		err = serveAttempt(c.dial, c.ch, listener)
		if c.MaxAttempts > 0 && attemptCount >= c.MaxAttempts {
			break
		}
		time.Sleep(time.Duration(c.Delay) * time.Second)
	}
	return
}

func serveAttempt(dial func() (*amqp.Connection, error), closing chan *struct{}, listener MessageListener) error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() {
		for {
			select {
			case <-closing:
				conn.Close()
			default:
			}
		}
	}()

	connErr := make(chan *amqp.Error, 1)
	forever := make(chan struct{})
	exErr := make(chan *amqp.Error, 1)

	go func() {
		e := <-connErr
		if e == nil {
			e = &amqp.Error{Reason: "<nil> connection error"}
		}
		exErr <- e
		defer close(exErr)
		close(forever)
	}()

	conn.NotifyClose(connErr)

	go listener.Listen(conn, forever)

	<-forever

	return <-exErr
}
