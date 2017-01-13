package amqpirq

import (
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/streadway/amqp"
	"time"
)

const (
	defaultMaxAttempts = -1
	defaultDelay       = uint(30)
)

var (
	// NamedReplyQueue is a lambda for a amqp.Queue qn definition on
	// amqp.Channel ch. The queue is defined as non-durable, non-exclusive
	NamedReplyQueue = func(ch *amqp.Channel, qn string) (amqp.Queue, error) {
		return ch.QueueDeclare(
			qn,    // name
			false, // durable
			false, // delete when unused
			false, // exclusive
			false, // no-wait
			nil,   // arguments
		)
	}
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

// Close sends a close signal to all MessageListener-s
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

// DeliveryConsumer is an interface for handling amqp.Delivery messages
// consumed from amqp.Channel on a queue
type DeliveryConsumer interface {
	Consume(*amqp.Channel, *amqp.Delivery)
}

// ParallelMessageListener is a parallel and asynchronous implementation of
// MessageListener
type ParallelMessageListener struct {
	consumer DeliveryConsumer
	size     int
	qn       string
}

// NewParallelMessageListener returns new MessageListener with a fixed pool
// size - size for the queue qn. Inbound messages are processed using
// DeliveryConsumer consumer.
func NewParallelMessageListener(qn string, size int, consumer DeliveryConsumer) (MessageListener, error) {
	if size < 1 {
		return nil, errors.New("amqpirq: pool size is required")
	}
	if qn == "" {
		return nil, errors.New("amqpirq: queue name is required")
	}
	return ParallelMessageListener{
		consumer: consumer,
		size:     size,
		qn:       qn,
	}, nil
}

func (ml ParallelMessageListener) Listen(conn *amqp.Connection, alive <-chan struct{}) {
	ch, err := conn.Channel()
	failOnError(err, "failed to open a channel")
	defer ch.Close()

	q, err := NamedReplyQueue(ch, ml.qn)
	failOnError(err, "failed to declare a queue")

	err = ch.Qos(
		ml.size,
		0,
		false,
	)
	failOnError(err, "failed to set QoS")

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	failOnError(err, "failed to register a consumer")

	wrkCh := make(chan amqp.Delivery, ml.size)

	for i := 0; i < ml.size; i++ {
		go func() {
			for {
				select {
				case d := <-wrkCh:
					ml.consumer.Consume(ch, &d)
				case <-alive:
					// avoid leaking goroutines
					return
				}
			}
		}()
	}

	for d := range msgs {
		wrkCh <- d
	}
	<-alive
}

func failOnError(err error, msg string) {
	if err != nil {
		panic(fmt.Errorf("amqpirq: %s: %v", msg, err))
	}
}
