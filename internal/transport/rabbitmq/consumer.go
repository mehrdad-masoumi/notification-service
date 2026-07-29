package rabbitmqtransport

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	notificationcontract "github.com/mehrdad-masoumi/broker-contract/go"
	"github.com/mehrdad-masoumi/go-packages/apperr"

	application "notification-service/internal/application/notification"
)

// Config holds RabbitMQ ingress topology for notification.requested.v1.
type Config struct {
	URI        string
	Exchange   string
	RoutingKey string
	Queue      string
	DLX        string
	DLQ        string
	Prefetch   int
}

// Consumer consumes notification.requested.v1 and delegates to CommandService.
type Consumer struct {
	cfg  Config
	cmds *application.CommandService
	conn *amqp.Connection
	ch   *amqp.Channel
}

func NewConsumer(cfg Config, cmds *application.CommandService) *Consumer {
	if cfg.Exchange == "" {
		cfg.Exchange = notificationcontract.CommandsExchange
	}
	if cfg.RoutingKey == "" {
		cfg.RoutingKey = notificationcontract.RequestedRouting
	}
	if cfg.Queue == "" {
		cfg.Queue = notificationcontract.RequestedQueue
	}
	if cfg.DLX == "" {
		cfg.DLX = notificationcontract.DLX
	}
	if cfg.DLQ == "" {
		cfg.DLQ = notificationcontract.RequestedDLQ
	}
	if cfg.Prefetch <= 0 {
		cfg.Prefetch = 10
	}
	return &Consumer{cfg: cfg, cmds: cmds}
}

func (c *Consumer) Start(ctx context.Context) error {
	conn, err := amqp.Dial(c.cfg.URI)
	if err != nil {
		return err
	}
	c.conn = conn
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return err
	}
	c.ch = ch
	if err := ch.Qos(c.cfg.Prefetch, 0, false); err != nil {
		return err
	}
	if err := c.declareTopology(ch); err != nil {
		return err
	}

	deliveries, err := ch.Consume(c.cfg.Queue, "notification-service.requested", false, false, false, false, nil)
	if err != nil {
		return err
	}

	log.Printf("rabbitmq consumer started queue=%s routing_key=%s", c.cfg.Queue, c.cfg.RoutingKey)

	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("rabbitmq deliveries channel closed")
			}
			c.handle(ctx, d)
		}
	}
}

func (c *Consumer) Stop(ctx context.Context) error {
	if c.ch != nil {
		_ = c.ch.Close()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Consumer) declareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(c.cfg.DLX, "fanout", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(c.cfg.DLQ, true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.QueueBind(c.cfg.DLQ, "", c.cfg.DLX, false, nil); err != nil {
		return err
	}
	if err := ch.ExchangeDeclare(c.cfg.Exchange, "topic", true, false, false, false, nil); err != nil {
		return err
	}
	args := amqp.Table{
		"x-dead-letter-exchange": c.cfg.DLX,
	}
	if _, err := ch.QueueDeclare(c.cfg.Queue, true, false, false, false, args); err != nil {
		return err
	}
	return ch.QueueBind(c.cfg.Queue, c.cfg.RoutingKey, c.cfg.Exchange, false, nil)
}

func (c *Consumer) handle(ctx context.Context, d amqp.Delivery) {
	cmdJSON, err := notificationcontract.ParseAndValidateJSON(d.Body)
	if err != nil {
		log.Printf("rabbitmq invalid payload → DLQ: %v", err)
		_ = d.Nack(false, false) // send to DLX
		return
	}
	if corr := strings.TrimSpace(d.CorrelationId); corr != "" && cmdJSON.CorrelationID == "" {
		cmdJSON.CorrelationID = corr
	}
	if trace, ok := headerString(d.Headers, "trace_id"); ok && cmdJSON.TraceID == "" {
		cmdJSON.TraceID = trace
	}

	cmd := mapContract(cmdJSON)
	_, err = c.cmds.Send(ctx, cmd)
	if err != nil {
		if isPermanent(err) {
			log.Printf("rabbitmq permanent failure → DLQ idempotency=%s: %v", redactKey(cmd.IdempotencyKey), err)
			_ = d.Nack(false, false)
			return
		}
		log.Printf("rabbitmq transient failure → requeue idempotency=%s: %v", redactKey(cmd.IdempotencyKey), err)
		time.Sleep(backoff(d.Headers))
		_ = d.Nack(false, true)
		return
	}
	_ = d.Ack(false)
}

func mapContract(n notificationcontract.NotificationRequested) application.SendNotificationCommand {
	channels := make([]application.Channel, 0, len(n.Channels))
	for _, ch := range n.Channels {
		channels = append(channels, application.Channel(ch))
	}
	return application.SendNotificationCommand{
		MessageID:      n.MessageID,
		IdempotencyKey: n.IdempotencyKey,
		SourceService:  n.SourceService,
		TemplateCode:   n.TemplateCode,
		Locale:         n.Locale,
		Recipient: application.Recipient{
			UserID:       n.Recipient.UserID,
			Email:        n.Recipient.Email,
			Phone:        n.Recipient.Phone,
			DeviceTokens: append([]string(nil), n.Recipient.DeviceTokens...),
			DisplayName:  n.Recipient.DisplayName,
		},
		Channels:      channels,
		Variables:     n.Variables,
		Metadata:      n.Metadata,
		ScheduledAt:   n.ScheduledAt,
		CorrelationID: n.CorrelationID,
		TraceID:       n.TraceID,
	}
}

func isPermanent(err error) bool {
	var ve *apperr.Error
	if errors.As(err, &ve) && ve != nil && len(ve.Fields) > 0 {
		return true
	}
	var re *apperr.RichError
	if errors.As(err, &re) && re != nil {
		switch re.Kind() {
		case apperr.KindInvalid, apperr.KindNotFound, apperr.KindForbidden:
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "validation")
}

func headerString(h amqp.Table, key string) (string, bool) {
	if h == nil {
		return "", false
	}
	v, ok := h[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func backoff(h amqp.Table) time.Duration {
	// Simple fixed backoff; attempt count may live in x-death later.
	_ = h
	return 500 * time.Millisecond
}

func redactKey(k string) string {
	if len(k) <= 8 {
		return "***"
	}
	return k[:4] + "…"
}

// PublishExample is unused at runtime; kept for tests documenting confirm usage.
func PublishExample(ch *amqp.Channel, exchange, routingKey string, body []byte) error {
	if err := ch.Confirm(false); err != nil {
		return err
	}
	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	if err := ch.PublishWithContext(context.Background(), exchange, routingKey, true, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
		Timestamp:    time.Now().UTC(),
	}); err != nil {
		return err
	}
	select {
	case c := <-confirms:
		if !c.Ack {
			return errors.New("publish not confirmed")
		}
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("publish confirm timeout")
	}
}

// MustMarshal is a test helper.
func MustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
