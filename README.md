# Interruptible connectivity for Go RabbitMQ Client Library

A wrapper for [Go RabbitMQ Client Library](https://github.com/streadway/amqp) 
providing auto-reconnecting functionality to `amqp.Connection`.

Maximum number of re-connect attempts can be configured by setting `int` value 
`conn.MaxAttempts`. By default the value is unlimited (`-1`). After a network
failure or any other connection issue, an attempt is going to be made to 
connect after `conn.Delay` seconds (by default `30`).

It is possible to start multiple listeners on a single connection in separate
goroutines. The underlying `amqp.Connection` is expected to be thread safe.

An interruptible connection can be configured using one of three `Dial[...]` 
functions that map to related `amqp.Dial[...]` functions respectively: 
`amqpirq.Dial`, `amqpirq.DialTLS` and `amqpirq.DialConfig`.


## Status

*Beta*

[![Build Status](https://travis-ci.org/go-amqpirq/amqpirq.svg?branch=master)](https://travis-ci.org/go-amqpirq/amqpirq) [![Coverage Status](https://coveralls.io/repos/github/go-amqpirq/amqpirq/badge.svg?branch=master)](https://coveralls.io/github/go-amqpirq/amqpirq?branch=master) [![GoDoc](https://godoc.org/github.com/go-amqpirq/amqpirq?status.svg)](https://godoc.org/github.com/go-amqpirq/amqpirq)


## Usage

### Go get

~~~
go get -u github.com/go-amqpirq/amqpirq
~~~

### Import

~~~go
import "github.com/go-amqpirq/amqpirq"
~~~

### Examples

Depending on level of control required by implementing application, it is 
possible to integrate `amqpirq` with business logic for `amqp.Delivery`,
`amqp.Channel` or `amqp.Connection`


#### `amqp.Delivery`

Implement `amqpirq.DeliveryConsumer` interface providing your business logic to
handle inbound `amqp.Delivery`. NOTE: *remember to acknowledge the delivery by 
invoking `d.Ack` or `d.Reject` as required by the business flow of your 
application*:

~~~go
type MyDeliveryConsumer struct {
}
 
func (*MyDeliveryConsumer) Consume(ch *amqp.Channel, d *amqp.Delivery) {
        defer d.Ack(false)
        doStuff(&d)
}
~~~

Configure parallel connection worker for the consumer `MyDeliveryConsumer`, e.g.:

~~~go
...
conn, err := amqpirq.Dial("amqp://guest:guest@127.0.0.1:5672//")
if err != nil {
        panic(err)
}
defer conn.Close()
 
consumer := new(MyDeliveryConsumer)
queueMaker := func(ch *amqp.Channel) (amqp.Queue, error) { return amqpirq.NamedReplyQueue(ch, "work_queue") }
numWorkers := 16
worker, err := NewParallelConnectionWorker(queueMaker, numWorkers, consumer)
if err != nil {
        panic(err)
}
go conn.Listen(worker)
...
~~~

#### `amqp.Channel`

Implement `amqpirq.ChannelWorker` interface to integrate requirements for
interacting directly with `amqp.Channel`, e.g.:

~~~go
type MyChannelWorker struct {
}
 
func (*MyChannelWorker) Do(ch *amqp.Channel, done <-chan struct{}) {
        // do things with channel
        <-done
}
~~~

Configure connection worker for the channel processor `MyChannelWorker`, e.g.:

~~~go
...
conn, err := amqpirq.Dial("amqp://guest:guest@127.0.0.1:5672//")
if err != nil {
        panic(err)
}
defer conn.Close()
 
processor := new(MyChannelWorker)
worker := NewConnectionWorker(processor)
if err != nil {
        panic(err)
}
go conn.Listen(worker)
...
~~~

#### `amqp.Connection`

Implement `amqpirq.ConnectionWorker` interface and start
`amqpirq.Connection.Listen` using specialised worker, e.g.:

~~~go
type MyConnectionWorker struct {
}
 
func (*MyConnectionWorker) Do(conn *amqp.Connection, done <-chan struct{}) {
        // to things with connection
        <-done
}

...
conn, err := amqpirq.Dial("amqp://guest:guest@127.0.0.1:5672//")
if err != nil {
        panic(err)
}
defer conn.Close()
 
go conn.Listen(new(MyConnectionWorker))
...
~~~

## Testing

To run all the tests, make sure you have RabbitMQ running on a host, export the
environment variable `AMQP_URI=` and run `go test -v ./...`.


## License

Apache License v2 - see LICENSE for more details.
