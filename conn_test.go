package amqpirq

import (
	"github.com/satori/go.uuid"
	"github.com/streadway/amqp"
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"
)

type dummyConnWorker struct {
	started bool
	ended   bool
}

func (d *dummyConnWorker) Do(conn *amqp.Connection, done <-chan struct{}) {
	d.started = true
	<-done
	d.ended = true
}

type dummyDeliveryConsumer struct {
	corrID *string
}

func (c *dummyDeliveryConsumer) Consume(ch *amqp.Channel, d *amqp.Delivery) {
	c.corrID = &d.CorrelationId
	d.Ack(false)
}

type dummyChanWorker struct {
	started bool
	ended   bool
}

func (d *dummyChanWorker) Do(conn *amqp.Channel, done <-chan struct{}) {
	d.started = true
	<-done
	d.ended = true
}

func TestDial(t *testing.T) {
	c, err := Dial("")
	if err != nil {
		t.Fatal(err)
	}
	if c.done == nil {
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
	if c.done == nil {
		t.Error("Expected chan got <nil>")
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
	if c.done == nil {
		t.Error("Expected chan got <nil>")
	}
	if got, want := c.MaxAttempts, defaultMaxAttempts; got != want {
		t.Errorf("Expected MaxAttempts=%d, got=%d", want, got)
	}
	if got, want := c.Delay, defaultDelay; got != want {
		t.Errorf("Expected MaxAttempts=%d, got=%d", want, got)
	}
	c.MaxAttempts = 2
	c.Delay = 0

	worker := new(dummyConnWorker)
	worker1 := new(dummyConnWorker)
	go c.Listen(worker)
	go c.Listen(worker1)

	for {
		if worker.started {
			break
		}
		time.Sleep(1 * time.Second)
	}
	for {
		if worker1.started {
			break
		}
		time.Sleep(1 * time.Second)
	}
	c.Close()
	for {
		if worker.ended {
			break
		}
		time.Sleep(1 * time.Second)
	}
	for {
		if worker1.ended {
			break
		}
		time.Sleep(1 * time.Second)
	}
}

func TestConnection_ListenOnClosed(t *testing.T) {
	if amqpURI() == "" {
		t.Skip("Environment variable AMQP_URI not set")
	}
	c, err := Dial(amqpURI())
	if err != nil {
		t.Fatal(err)
	}
	if c.done == nil {
		t.Error("Expected chan got <nil>")
	}
	if got, want := c.MaxAttempts, defaultMaxAttempts; got != want {
		t.Errorf("Expected MaxAttempts=%d, got=%d", want, got)
	}
	if got, want := c.Delay, defaultDelay; got != want {
		t.Errorf("Expected MaxAttempts=%d, got=%d", want, got)
	}
	c.MaxAttempts = 1
	c.Delay = 0
	c.Close()
	if got, want := c.closing, true; got != want {
		t.Errorf("Expected closing=%b, got=%b", want, got)
	}
	c.Close()
	if got, want := c.closing, true; got != want {
		t.Errorf("Expected closing=%b, got=%b", want, got)
	}

	worker := new(dummyConnWorker)
	err = c.Listen(worker)
	if err == nil {
		t.Fatal("Expected error got <nil>")
	}
}

func TestConnection_ListenInvalidURI(t *testing.T) {
	c, err := DialConfig("amqp://non-existent-host//", amqp.Config{})
	if err != nil {
		t.Fatal(err)
	}
	c.MaxAttempts = 2
	c.Delay = 0
	err = c.Listen(new(dummyConnWorker))
	if err == nil {
		t.Fatal("Expected error got <nil>")
	}
}

func TestNewParallelMessageListener_InvalidSize(t *testing.T) {
	_, err := NewParallelConnectionWorker(nil, 0, nil)
	if err == nil {
		t.Fatal("Expected error, got <nil>")
	}
}

func TestNewParallelMessageListener_MissingQueue(t *testing.T) {
	_, err := NewParallelConnectionWorker(nil, 1, nil)
	if err == nil {
		t.Fatal("Expected error, got <nil>")
	}
}

func TestConnection_NewConnectionWorker(t *testing.T) {
	if amqpURI() == "" {
		t.Skip("Environment variable AMQP_URI not set")
	}

	c, err := Dial(amqpURI())
	if err != nil {
		t.Fatal(err)
	}
	if c.done == nil {
		t.Error("Expected chan got <nil>")
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

	processor := new(dummyChanWorker)
	worker := NewConnectionWorker(processor)
	go c.Listen(worker)
	for {
		if processor.started {
			break
		}
	}
	c.Close()
	for {
		if processor.ended {
			break
		}
	}
}

func TestConnection_NewParallelConnectionWorker(t *testing.T) {
	if amqpURI() == "" {
		t.Skip("Environment variable AMQP_URI not set")
	}
	c, err := Dial(amqpURI())
	if err != nil {
		t.Fatal(err)
	}
	if c.done == nil {
		t.Error("Expected chan got <nil>")
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
	tempQ := uuid.NewV4().String()
	qMaker := func(ch *amqp.Channel) (amqp.Queue, error) {
		return ch.QueueDeclare(tempQ, false, false, false, false, nil)
	}
	_, err = qMaker(ch)
	if err != nil {
		t.Fatal(err)
	}
	defer ch.QueueDelete(tempQ, false, false, false)

	consumer := new(dummyDeliveryConsumer)
	worker, err := NewParallelConnectionWorker(qMaker, 1, consumer)
	go func() {
		err := c.Listen(worker)
		if got, want := err.Error(), `Exception (0) Reason: "<nil> connection error"`; got != want {
			t.Errorf("Expected error='%s', got='%s'", want, got)
		}
	}()

	corrID := uuid.NewV4().String()

	err = ch.Publish("", tempQ, false, false, amqp.Publishing{
		CorrelationId: corrID,
		Body:          []byte(corrID),
	})
	if err != nil {
		t.Fatal(err)
	}
	for {
		if consumer.corrID != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if got, want := *consumer.corrID, corrID; got != want {
		t.Errorf("Expected CorrelationId='%s', got '%s'", want, got)
	}
	c.Close()
	// wait for internal channels to close
	time.Sleep(1 * time.Second)
}

func TestNewParallelConnectionWorkerBulk(t *testing.T) {
	if amqpURI() == "" {
		t.Skip("Environment variable AMQP_URI not set")
	}
	c, err := Dial(amqpURI())
	if err != nil {
		t.Fatal(err)
	}

	clientConn, err := amqp.Dial(amqpURI())
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()

	clientCh, err := clientConn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer clientCh.Close()

	tempQ := uuid.NewV4().String()
	qMaker := func(ch *amqp.Channel) (amqp.Queue, error) {
		return ch.QueueDeclare(tempQ, false, false, false, false, nil)
	}
	_, err = qMaker(clientCh)
	if err != nil {
		t.Fatal(err)
	}
	defer clientCh.QueueDelete(tempQ, false, false, false)
	poolSize := 5 * runtime.NumCPU()

	consumer := new(countConsumer)
	worker, err := NewParallelConnectionWorker(qMaker, poolSize, consumer)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		err := c.Listen(worker)
		if got, want := err.Error(), `Exception (0) Reason: "<nil> connection error"`; got != want {
			t.Errorf("Expected error='%s', got='%s'", want, got)
		}
	}()

	batchSize := 100 * runtime.NumCPU()
	for i := 0; i < batchSize; i++ {
		err := clientCh.Publish("", tempQ, false, false, amqp.Publishing{Body: []byte(strconv.FormatInt(int64(i), 10))})
		if err != nil {
			t.Fatal(err)
		}
	}

	for {
		if consumer.count == batchSize {
			break
		}
		time.Sleep(1 * time.Second)
	}

	c.Close()
	// wait for internal channels to close
	time.Sleep(1 * time.Second)

}

type countConsumer struct {
	mutex sync.Mutex
	count int
}

func (consumer *countConsumer) Consume(ch *amqp.Channel, d *amqp.Delivery) {
	consumer.mutex.Lock()
	defer consumer.mutex.Unlock()
	consumer.count++
	d.Ack(false)
}

func amqpURI() string { return os.Getenv("AMQP_URI") }
