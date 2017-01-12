# Interruptible connectivity for Go RabbitMQ Client Library

A wrapper for [Go RabbitMQ Client Library](https://github.com/streadway/amqp) 
providing auto-reconnecting functionality to `amqp.Connection`.

Maximum number of re-connect attempts can be configured by setting `int` value 
`conn.MaxAttempts`. By default the value is unlimited (`-1`). After a network
failure or any other connection issue, an attempt is going to be made to 
connect after `conn.Delay` seconds (by default `30`).

It is possible to start multiple listeners on a single connection in separate
goroutines. The underlying `amqp.Connection` is expected to be thread safe.

An interruptible connection can be configured using one of three `Dial` 
functions that map to related `amqp.Dial` functions: `amqpirq.Dial`, 
`amqpirq.DialTLS` and `amqpirq.DialConfig`.


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

Implement `amqpirq.MessageListener` sensitive to interruption of the connection, 
e.g. auto-ack should be avoided (deliveries should be acknowledged only after 
successful processing), check status of the `chan` and interrupt routines 
after the channel had been closed:

~~~go
type AsyncMessageListener struct {
}
 
func (*AsyncMessageListener) Listen(conn *amqp.Connection, alive <-chan struct{}) {
        ch, err := conn.Channel()
        if err != nil {
                panic(err)
        }
        defer ch.Close()
        
        ...
        
        wrkCh := make(chan amqp.Delivery, 1)
        
        msgs, err := ch.Consume(
                q.Name, // queue
                "",     // consumer
                false,  // auto-ack
                false,  // exclusive
                false,  // no-local
                false,  // no-wait
                nil,    // args
        )
        if err != nil {
                panic(err)
        }
        		
        go func() {
                for {
                        select {
                        case d := <-wrkCh:
                                if err := processDelivery(d.Body); err != nil {
                                        d.Reject(false)
                                } else {
                                        d.Ack(false)
                                }
                        case <-alive:
                                // avoid leaking goroutines
                                return
                        }
                }
        }()
        
        for d := range msgs {
                wrkCh <- d
        }
        
        <-alive
}
~~~

Set-up interruptible connection and start listener(s)

~~~go
...
conn, err := amqpirq.Dial("amqp://guest:guest@127.0.0.1:5672//")
if err != nil {
        panic(err)
}
defer conn.Close()
 
listener := new(AsyncMessageListener)
go conn.Listen(listener)
...
~~~


## Testing

To run all the tests, make sure you have RabbitMQ running on a host, export the
environment variable `AMQP_URI=` and run `go test -v ./...`.


## License

Apache License v2 - see LICENSE for more details.
