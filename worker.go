package thumper

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dstrop/thumper/amqp"
	"github.com/dstrop/thumper/common"
	"github.com/roadrunner-server/errors"
	"github.com/roadrunner-server/goridge/v3/pkg/frame"
	"github.com/roadrunner-server/pool/payload"
	"go.uber.org/zap"
	"sync"
)

type Worker struct {
	log  *zap.Logger
	pool common.Pool

	wwg sync.WaitGroup
	wg  sync.WaitGroup

	messageCh chan *message
}

func NewWorkerPool(pool common.Pool, workerCount int, logger *zap.Logger) *Worker {
	w := &Worker{
		pool:      pool,
		messageCh: make(chan *message),
		log:       logger,
	}

	for i := 0; i < workerCount; i++ {
		go w.worker(w.messageCh)
	}

	return w
}

func (w *Worker) Consume(deliveries <-chan amqp.Delivery, consumer *ConsumerConfig) {
	if consumer.Concurrency > 0 {
		go w.concurrencyConsumer(deliveries, consumer)
	} else {
		go w.consumer(deliveries, consumer)
	}
}

func (w *Worker) WaitClose() {
	w.wg.Wait()
	close(w.messageCh)
	w.wwg.Wait()
}

func (w *Worker) consumer(deliveries <-chan amqp.Delivery, consumer *ConsumerConfig) {
	w.wg.Add(1)
	defer w.wg.Done()

	for delivery := range deliveries {
		w.messageCh <- &message{
			delivery: &delivery,
			consumer: consumer,
		}
	}
}

func (w *Worker) concurrencyConsumer(deliveries <-chan amqp.Delivery, consumer *ConsumerConfig) {
	w.wg.Add(1)
	defer w.wg.Done()

	semaphore := make(chan struct{}, consumer.Concurrency)

	for delivery := range deliveries {
		semaphore <- struct{}{}

		w.messageCh <- &message{
			delivery:  &delivery,
			consumer:  consumer,
			semaphore: semaphore,
		}
	}
}

const (
	Ack    = '0'
	Nack   = '1'
	Reject = '2'
)

type message struct {
	delivery *amqp.Delivery
	consumer *ConsumerConfig

	semaphore chan struct{}
}

func (w *Worker) worker(messageCh <-chan *message) {
	w.wwg.Add(1)
	defer w.wwg.Done()

	for msg := range messageCh {
		w.doWork(msg)
	}
}

func (w *Worker) doWork(msg *message) {
	if msg.semaphore != nil {
		defer func() {
			<-msg.semaphore
		}()
	}

	if msg.delivery.Acknowledger.(interface {
		IsClosed() bool
	}).IsClosed() {
		return
	}

	pld, err := createPayload(msg.delivery, msg.consumer)
	if err != nil {
		w.workFailed(msg, "failed to create payload", err)
		return
	}

	result, err := w.exec(pld)
	if err != nil {
		w.workFailed(msg, "failed to execute payload", err)
		return
	}

	if len(result.Body) != 1 {
		w.workFailed(msg, "malformed response body", errors.Str("malformed response body"))
		return
	}

	switch result.Body[0] {
	case Ack:
		err = msg.delivery.Ack(false)
	case Nack:
		err = msg.delivery.Nack(false, true)
	case Reject:
		err = msg.delivery.Reject(false)
	}

	if err != nil {
		w.log.Error("failed to ack message", zap.Error(err))
	}
}

func (w *Worker) workFailed(msg *message, logMsg string, err error) {
	w.log.Error(logMsg, zap.Error(err))

	err = msg.delivery.Nack(false, *msg.consumer.RequeueOnFail)
	if err != nil {
		w.log.Error("failed to nack message", zap.Error(err))
	}
}

func createPayload(delivery *amqp.Delivery, consumer *ConsumerConfig) (*payload.Payload, error) {
	pld := &payload.Payload{
		Body: delivery.Body,
		// TODO: add support for other codecs
		Codec: frame.CodecJSON,
	}
	msgContext := map[string]interface{}{
		"queue":       consumer.Queue,
		"headers":     delivery.Headers,
		"exchange":    delivery.Exchange,
		"routingKey":  delivery.RoutingKey,
		"deliveryTag": delivery.DeliveryTag,
	}

	var err error
	pld.Context, err = json.Marshal(msgContext)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message headers: %w", err)
	}

	return pld, nil
}

func (w *Worker) exec(pld *payload.Payload) (*payload.Payload, error) {
	resultCh, err := w.pool.Exec(context.Background(), pld, nil)
	if err != nil {
		return nil, err
	}

	// TODO: add timeout and kill worker if it's not responding
	select {
	case result := <-resultCh:
		if result.Error() != nil {
			return nil, result.Error()
		}

		if result.Payload().Flags&frame.STREAM != 0 {
			return nil, errors.Str("streaming is not supported")
		}

		return result.Payload(), nil
	default:
		return nil, errors.Str("worker empty response")
	}
}
