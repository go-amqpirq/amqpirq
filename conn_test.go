package amqpirq

import (
	"github.com/streadway/amqp"
	"os"
	"testing"
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

	l := new(dummyListener)
	go c.Listen(l)

	for {
		if l.started {
			break
		}
	}
	c.Close()
	for {
		if l.ended {
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

func amqpURI() string { return os.Getenv("AMQP_URI") }
