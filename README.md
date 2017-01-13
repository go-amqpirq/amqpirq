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

[![Build Status](https://travis-ci.org/go-amqpirq/amqpirq.svg?branch=master)](https://travis-ci.org/go-amqpirq/amqpirq) [![Coverage Status](https://coveralls.io/repos/github/go-amqpirq/amqpirq/badge.svg?branch=master)](https://coveralls.io/github/go-amqpirq/amqpirq?branch=master)


## Usage

### Go get

~~~
go get gopkg.in/amqpirq.v0
~~~

### Import

~~~go
import "gopkg.in/amqpirq.v0"
~~~

### Example

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

Configure parallel worker for the consumer, e.g.:

~~~go
...
conn, err := amqpirq.Dial("amqp://guest:guest@127.0.0.1:5672//")
if err != nil {
        panic(err)
}
defer conn.Close()
 
consumer := new(DeliveryConsumer)
queueName := "work_queue"
numWorkers := 16
worker := NewParallelMessageWorker(queueName, numWorkers, consumer)
go conn.Listen(worker)
...
~~~

Alternatively, implement `amqpirq.MessageWorker` interface and start
`amqpirq.Connection.Listen` using specialised worker, e.g.:

~~~go
type MyMessageWorker struct {
}
 
func (*MyMessageWorker) Do(conn *amqp.Connection, done <-chan struct{}) {
        for {
                select {
                case <-done:
                        return
                default:
                        ...
                }
        }
}
~~~

## Testing

To run all the tests, make sure you have RabbitMQ running on a host, export the
environment variable `AMQP_URI=` and run `go test -v ./...`.


## License

Apache License v2 - see LICENSE for more details.
