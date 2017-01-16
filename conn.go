package amqpirq

import (
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/streadway/amqp"
	"runtime"
	"sync"
	"time"
)

const (
	defaultMaxAttempts = -1
	defaultDelay       = uint(30)
)

var (
	// NamedReplyQueue is a lambda for a amqp.Queue key definition on
	// amqp.Channel ch. The queue is defined as durable, non-exclusive
	NamedReplyQueue = func(ch *amqp.Channel, key string) (amqp.Queue, error) {
		return ch.QueueDeclare(
			key,   // name
			true,  // durable
			false, // delete when unused
			false, // exclusive
			false, // no-wait
			nil,   // arguments
		)
	}
)

// ConnectionWorker is enabling configuring channels and queues to implement
// client code depending on amqp.Connection to the broker. The Do function is
// usually invoked in a goroutine and the channel is closed when either a
// connection to the broker is lost or the amqpirq.Connection is closed.
type ConnectionWorker interface {
	Do(*amqp.Connection, <-chan struct{})
}

// ChannelWorker is enabling configuring queues to implement client code
// depending on amqp.Channel. The Do function is usually invoked in a goroutine
// and the channel is closed when either a connection to the broker is lost or
// the amqpirq.Connection is closed.
type ChannelWorker interface {
	Do(*amqp.Channel, <-chan struct{})
}

// Connection facilitates interruptible connectivity to RabbitMQ broker,
// re-attempting connects until max attempts (default unlimited) with
// configured delay (default 30 secods)
type Connection struct {
	// MaxAttempts is after how many unsuccessful attempts an error
	// is returned
	MaxAttempts int
	// Delay is number of seconds to wait before re-attempting connection
	Delay   uint
	done    chan struct{}
	dial    func() (*amqp.Connection, error)
	closing bool
	lasterr error
	mutex   sync.Mutex
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
		done:        make(chan struct{}),
	}, nil

}

// Close sends a done signal to all workers
func (conn *Connection) Close() {
	conn.mutex.Lock()
	defer conn.mutex.Unlock()
	if conn.closing {
		return
	}
	conn.closing = true
	close(conn.done)
}

// LastError returns last connection error (if any)
func (conn *Connection) LastError() error {
	conn.mutex.Lock()
	defer conn.mutex.Unlock()
	return conn.lasterr
}

func (conn *Connection) seterr(err error) {
	conn.mutex.Lock()
	defer conn.mutex.Unlock()
	conn.lasterr = err
}

func (conn *Connection) Listen(worker ConnectionWorker) (err error) {
	conn.mutex.Lock()
	if conn.closing {
		return errors.New("amqpirq: unable to listen on closed connection")
	}
	conn.mutex.Unlock()
	attemptCount := 0
	for {
		attemptCount++
		err = serveAttempt(conn.dial, conn.done, worker)
		conn.seterr(err)
		if conn.MaxAttempts > 0 && attemptCount >= conn.MaxAttempts {
			break
		}
		conn.mutex.Lock()
		if conn.closing {
			break
		}
		conn.mutex.Unlock()
		time.Sleep(time.Duration(conn.Delay) * time.Second)
	}
	return
}

func serveAttempt(dial func() (*amqp.Connection, error), done <-chan struct{}, worker ConnectionWorker) error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() {
		for {
			select {
			case <-done:
				conn.Close()
			default:
			}
		}
	}()

	connErr := make(chan *amqp.Error, 1)
	forever := make(chan struct{})
	exitErr := make(chan error, 1)

	go func() {
		e := <-connErr
		if e == nil {
			exitErr <- nil
		} else {
			exitErr <- errors.New(e.Reason)
		}
		defer close(exitErr)
		close(forever)
	}()

	conn.NotifyClose(connErr)

	go worker.Do(conn, forever)

	<-forever

	return <-exitErr
}

// DeliveryConsumer is an interface for handling amqp.Delivery messages
// consumed from amqp.Channel on a queue
type DeliveryConsumer interface {
	Consume(*amqp.Channel, *amqp.Delivery)
}

// FixedChannelWorker is a fixed prefetch parallel processing worker
type FixedChannelWorker struct {
	queue    func(ch *amqp.Channel) (amqp.Queue, error)
	size     int
	consumer DeliveryConsumer
}

// NewFixedChannelWorker configures fixed size number of workers on amqp.Queue
// configured using NamedReplyQueue with name key
func NewFixedChannelWorkerName(key string, size int, consumer DeliveryConsumer) ChannelWorker {
	return FixedChannelWorker{
		size:     size,
		consumer: consumer,
		queue:    func(ch *amqp.Channel) (amqp.Queue, error) { return NamedReplyQueue(ch, key) },
	}
}

// NewFixedChannelWorker configures fixed size number of workers on amqp.Queue
// declared by lambda queue
func NewFixedChannelWorker(queue func(ch *amqp.Channel) (amqp.Queue, error), size int, consumer DeliveryConsumer) ChannelWorker {
	return FixedChannelWorker{
		size:     size,
		consumer: consumer,
		queue:    queue,
	}
}

func (worker FixedChannelWorker) Do(ch *amqp.Channel, done <-chan struct{}) {
	q, err := worker.queue(ch)
	failOnError(err, "failed to declare a queue")

	err = ch.Qos(
		worker.size,
		0,
		false,
	)
	failOnError(err, "failed to set QoS")

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		false,  // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	failOnError(err, "failed to register a consumer")

	wrkCh := make(chan amqp.Delivery, worker.size*runtime.NumCPU())

	for i := 0; i < worker.size; i++ {
		go func() {
			for {
				select {
				case d := <-wrkCh:
					worker.consumer.Consume(ch, &d)
				case <-done:
					// avoid leaking goroutines
					return
				}
			}
		}()
	}

	for d := range msgs {
		wrkCh <- d
	}
	<-done
}

// ParallelConnectionWorker is a parallel and asynchronous implementation of
// ConnectionWorker
type ParallelConnectionWorker struct {
	processor ChannelWorker
}

// NewConnectionWorker returns new ConnectionWorker with .
func NewConnectionWorker(worker ChannelWorker) ConnectionWorker {
	return ParallelConnectionWorker{
		processor: worker,
	}
}

// NewParallelConnectionWorker returns new ConnectionWorker with a fixed pool
// size for the queue key configured using NamedReplyQueue. Inbound messages
// are processed using DeliveryConsumer consumer.
func NewParallelConnectionWorkerName(key string, size int, consumer DeliveryConsumer) (ConnectionWorker, error) {
	return NewParallelConnectionWorker(
		func(ch *amqp.Channel) (amqp.Queue, error) { return NamedReplyQueue(ch, key) },
		size,
		consumer,
	)
}

// NewParallelConnectionWorker returns new ConnectionWorker with a fixed pool
// size for the queue configured using lambda queue. Inbound messages are
// processed using DeliveryConsumer consumer.
func NewParallelConnectionWorker(queue func(ch *amqp.Channel) (amqp.Queue, error), size int, consumer DeliveryConsumer) (ConnectionWorker, error) {
	if size < 1 {
		return nil, errors.New("amqpirq: pool size is required")
	}
	if queue == nil {
		return nil, errors.New("amqpirq: queue maker is required")
	}
	processor := NewFixedChannelWorker(queue, size, consumer)
	return ParallelConnectionWorker{
		processor: processor,
	}, nil
}

func (worker ParallelConnectionWorker) Do(conn *amqp.Connection, done <-chan struct{}) {
	ch, err := conn.Channel()
	failOnError(err, "failed to open a channel")
	defer ch.Close()

	worker.processor.Do(ch, done)
}

func failOnError(err error, msg string) {
	if err != nil {
		panic(fmt.Errorf("amqpirq: %s: %v", msg, err))
	}
}
