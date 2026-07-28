package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"notification-service/config"
	notificationdto "notification-service/internal/notification/dto"
	"notification-service/internal/notification/entity"
	notificationmetrics "notification-service/internal/notification/metrics"
)

const (
	exchangeName = "notification"
	// confirmTimeout bounds how long PublishWithConfirm waits for a
	// broker ack/nack (or a mandatory-publish return) before treating the
	// publish as failed/unknown and letting the caller retry.
	confirmTimeout = 5 * time.Second
)

var priorities = []entity.Priority{
	entity.PriorityHigh,
	entity.PriorityNormal,
	entity.PriorityLow,
}

func QueueName(p entity.Priority) string {
	return "notification." + string(p)
}

func DLQName(p entity.Priority) string {
	return "notification." + string(p) + ".dlq"
}

type Client struct {
	cfg  config.Rabbitmq
	conn *amqp.Connection
	mu   sync.Mutex
}

func NewClient(cfg config.Rabbitmq) (*Client, error) {
	c := &Client{cfg: cfg}
	if err := c.connect(); err != nil {
		return nil, err
	}
	if err := c.SetupTopology(); err != nil {
		return nil, err
	}
	go c.handleReconnect()
	return c, nil
}

func (c *Client) connect() error {
	conn, err := amqp.Dial(c.cfg.URI())
	if err != nil {
		return fmt.Errorf("rabbitmq dial: %w", err)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	return nil
}

func (c *Client) handleReconnect() {
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			time.Sleep(5 * time.Second)
			continue
		}
		errCh := conn.NotifyClose(make(chan *amqp.Error, 1))
		err := <-errCh
		log.Printf("rabbitmq connection closed: %v; reconnecting", err)
		for {
			if err := c.connect(); err != nil {
				log.Printf("rabbitmq reconnect failed: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}
			if err := c.SetupTopology(); err != nil {
				log.Printf("rabbitmq topology setup failed: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}
			log.Printf("rabbitmq reconnected")
			break
		}
	}
}

func (c *Client) channel() (*amqp.Channel, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil || conn.IsClosed() {
		return nil, fmt.Errorf("rabbitmq not connected")
	}
	return conn.Channel()
}

func (c *Client) SetupTopology() error {
	ch, err := c.channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	if err := ch.ExchangeDeclare(exchangeName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}

	for _, p := range priorities {
		dlq := DLQName(p)
		qName := QueueName(p)
		routing := qName

		if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare dlq %s: %w", dlq, err)
		}

		args := amqp.Table{
			"x-dead-letter-exchange":    exchangeName,
			"x-dead-letter-routing-key": dlq,
		}
		if _, err := ch.QueueDeclare(qName, true, false, false, false, args); err != nil {
			return fmt.Errorf("declare queue %s: %w", qName, err)
		}
		if err := ch.QueueBind(qName, routing, exchangeName, false, nil); err != nil {
			return fmt.Errorf("bind queue %s: %w", qName, err)
		}
		if err := ch.QueueBind(dlq, dlq, exchangeName, false, nil); err != nil {
			return fmt.Errorf("bind dlq %s: %w", dlq, err)
		}
	}
	return nil
}

// Publish is a fire-and-forget publish (persistent, non-mandatory). Used by
// the worker to re-enqueue a job for immediate retry after a temporary
// failure; delivery is already tracked in Postgres so an occasional lost
// publish is caught by RecoverStuckSending/outbox reconciliation rather
// than blocking the retry path on a broker round-trip.
func (c *Client) Publish(ctx context.Context, priority entity.Priority, job notificationdto.QueueJob) error {
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	ch, err := c.channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	routing := QueueName(priority)
	return ch.PublishWithContext(ctx, exchangeName, routing, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
		Headers: amqp.Table{
			"x-attempt": job.Attempt,
		},
	})
}

// PublishWithConfirm publishes with the channel in confirm mode and
// mandatory=true, blocking until the broker acknowledges the message (or
// returns it as unroutable) or confirmTimeout elapses. This is the
// reliable publish path used by the outbox publisher, so a message is
// only marked "published" in Postgres once RabbitMQ has actually
// persisted/routed it.
func (c *Client) PublishWithConfirm(ctx context.Context, routingKey string, body []byte) error {
	ch, err := c.channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	if err := ch.Confirm(false); err != nil {
		return fmt.Errorf("enable confirm mode: %w", err)
	}

	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	returns := ch.NotifyReturn(make(chan amqp.Return, 1))

	err = ch.PublishWithContext(ctx, exchangeName, routingKey, true, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
	if err != nil {
		notificationmetrics.IncOutboxPublish("publish_error")
		return fmt.Errorf("publish: %w", err)
	}

	timer := time.NewTimer(confirmTimeout)
	defer timer.Stop()

	select {
	case ret := <-returns:
		notificationmetrics.IncOutboxPublish("returned")
		return fmt.Errorf("message returned as unroutable: reply=%s routing_key=%s", ret.ReplyText, ret.RoutingKey)
	case conf, ok := <-confirms:
		if !ok {
			notificationmetrics.IncOutboxPublish("confirm_channel_closed")
			return fmt.Errorf("confirm channel closed before ack")
		}
		if !conf.Ack {
			notificationmetrics.IncOutboxPublish("nack")
			return fmt.Errorf("broker nacked publish")
		}
		notificationmetrics.IncOutboxPublish("success")
		return nil
	case <-timer.C:
		notificationmetrics.IncOutboxPublish("timeout")
		return fmt.Errorf("timed out waiting for publish confirmation")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) PublishToDLQ(ctx context.Context, priority entity.Priority, job notificationdto.QueueJob) error {
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	ch, err := c.channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	return ch.PublishWithContext(ctx, exchangeName, DLQName(priority), false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}

type DeliveryHandler func(ctx context.Context, job notificationdto.QueueJob, attempt int) error

func (c *Client) Consume(ctx context.Context, priority entity.Priority, prefetch int, handler DeliveryHandler) error {
	ch, err := c.channel()
	if err != nil {
		return err
	}

	if err := ch.Qos(prefetch, 0, false); err != nil {
		_ = ch.Close()
		return err
	}

	qName := QueueName(priority)
	deliveries, err := ch.Consume(qName, "", false, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		return err
	}

	go func() {
		<-ctx.Done()
		_ = ch.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("delivery channel closed for %s", qName)
			}
			var job notificationdto.QueueJob
			if err := json.Unmarshal(d.Body, &job); err != nil {
				log.Printf("invalid job payload on %s: %v", qName, err)
				_ = d.Nack(false, false) // to DLQ
				continue
			}
			attempt := job.Attempt
			if v, ok := d.Headers["x-attempt"]; ok {
				switch t := v.(type) {
				case int32:
					attempt = int(t)
				case int64:
					attempt = int(t)
				case int:
					attempt = t
				}
			}
			if err := handler(ctx, job, attempt); err != nil {
				log.Printf("handler error queue=%s delivery_id=%s: %v", qName, job.DeliveryID, err)
				_ = d.Nack(false, false)
				continue
			}
			_ = d.Ack(false)
		}
	}
}

func (c *Client) Ping(ctx context.Context) error {
	_ = ctx
	ch, err := c.channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
