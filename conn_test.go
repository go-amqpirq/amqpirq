package amqpirq

import (
	"github.com/streadway/amqp"
	"math/rand"
	"os"
	"testing"
	"time"
)

type dummyListener struct {
	started bool
	ended   bool
}

func (d *dummyListener) Listen(conn *amqp.Connection, ch <-chan struct{}) {
	d.started = true
	<-ch
	d.ended = true
}

func TestDial(t *testing.T) {
	c, err := Dial("")
	if err != nil {
		t.Fatal(err)
	}
	if cap(c.ch) == 0 {
		t.Error("Expected buffered chan got cap=")
	}
	if c.ch == nil {
		t.Error("Expected chan got <nil>")
	}
	if got, want := c.MaxAttempts, defaultMaxAttempts; got != want {
		t.Errorf("Expected MaxAttempts=%d, got=%d", want, got)
	}
	if got, want := c.Delay, defaultDelay; got != want {
		t.Errorf("Expected MaxAttempts=%d, got=%d", want, got)
	}
}

func TestDialTLS(t *testing.T) {
	c, err := DialTLS("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.ch == nil {
		t.Error("Expected chan got <nil>")
	}
	if cap(c.ch) == 0 {
		t.Error("Expected buffered chan got cap=0")
	}
	if got, want := c.MaxAttempts, defaultMaxAttempts; got != want {
		t.Errorf("Expected MaxAttempts=%d, got=%d", want, got)
	}
	if got, want := c.Delay, defaultDelay; got != want {
		t.Errorf("Expected MaxAttempts=%d, got=%d", want, got)
	}
}

func TestConnection_Listen(t *testing.T) {
	if amqpURI() == "" {
		t.Skip("Environment variable AMQP_URI not set")
	}
	c, err := Dial(amqpURI())
	if err != nil {
		t.Fatal(err)
	}
	if c.ch == nil {
		t.Error("Expected chan got <nil>")
	}
	if cap(c.ch) == 0 {
		t.Error("Expected buffered chan got cap=")
	}
	if got, want := c.MaxAttempts, defaultMaxAttempts; got != want {
		t.Errorf("Expected MaxAttempts=%d, got=%d", want, got)
	}
	if got, want := c.Delay, defaultDelay; got != want {
		t.Errorf("Expected MaxAttempts=%d, got=%d", want, got)
	}
	c.MaxAttempts = 1
	c.Delay = 0

	listener := new(dummyListener)
	go c.Listen(listener)

	for {
		if listener.started {
			break
		}
	}
	c.Close()
	for {
		if listener.ended {
			break
		}
	}
}

func TestConnection_ListenInvalidURI(t *testing.T) {
	c, err := DialConfig("amqp://non-existent-host//", amqp.Config{})
	if err != nil {
		t.Fatal(err)
	}
	c.MaxAttempts = 2
	c.Delay = 0
	err = c.Listen(new(dummyListener))
	if err == nil {
		t.Fatal("Expected error got <nil>")
	}
}

func TestNewParallelMessageListener_InvalidSize(t *testing.T) {
	_, err := NewParallelMessageListener("", 0, nil)
	if err == nil {
		t.Fatal("Expected error, got <nil>")
	}
}

func TestNewParallelMessageListener_MissingQueue(t *testing.T) {
	_, err := NewParallelMessageListener("", 1, nil)
	if err == nil {
		t.Fatal("Expected error, got <nil>")
	}
}

type dummyDeliveryConsumer struct {
	corrID *string
}

func (c *dummyDeliveryConsumer) Consume(ch *amqp.Channel, d *amqp.Delivery) {
	c.corrID = &d.CorrelationId
	d.Ack(false)
}

func TestConnection_ListenParallelMessageListener(t *testing.T) {
	if amqpURI() == "" {
		t.Skip("Environment variable AMQP_URI not set")
	}
	c, err := Dial(amqpURI())
	if err != nil {
		t.Fatal(err)
	}
	if c.ch == nil {
		t.Error("Expected chan got <nil>")
	}
	if cap(c.ch) == 0 {
		t.Error("Expected buffered chan got cap=")
	}
	if got, want := c.MaxAttempts, defaultMaxAttempts; got != want {
		t.Errorf("Expected MaxAttempts=%d, got=%d", want, got)
	}
	if got, want := c.Delay, defaultDelay; got != want {
		t.Errorf("Expected MaxAttempts=%d, got=%d", want, got)
	}
	c.MaxAttempts = 1
	c.Delay = 0

	conn, err := amqp.Dial(amqpURI())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	tempQ, err := NamedReplyQueue(ch, randomString(10))
	if err != nil {
		t.Fatal(err)
	}
	defer ch.QueueDelete(tempQ.Name, false, false, false)

	consumer := new(dummyDeliveryConsumer)
	listener, err := NewParallelMessageListener(tempQ.Name, 1, consumer)
	go c.Listen(listener)

	corrID := "XYZ"

	ch.Publish("", tempQ.Name, false, false, amqp.Publishing{
		CorrelationId: corrID,
		Body:          []byte(corrID),
	})

	for {
		if consumer.corrID != nil {
			break
		}
	}
	if got, want := *consumer.corrID, corrID; got != want {
		t.Errorf("Expected CorrelationId='%s', got '%s'", want, got)
	}
	c.Close()
	// wait for internal channels to close
	time.Sleep(1 * time.Second)
}

func amqpURI() string { return os.Getenv("AMQP_URI") }

func randomString(l int) string {
	bytes := make([]byte, l)
	for i := 0; i < l; i++ {
		bytes[i] = byte(randInt(65, 90))
	}
	return string(bytes)
}

func randInt(min int, max int) int {
	return min + rand.Intn(max-min)
}
