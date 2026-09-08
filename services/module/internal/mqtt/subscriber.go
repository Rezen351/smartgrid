package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/almuzky/iot/services/module/internal/model"
	"github.com/almuzky/iot/services/module/internal/service"
)

// Subscriber listens for firmware onboarding signals on Mosquitto.
type Subscriber struct {
	client      mqtt.Client
	svc         *service.ModuleService
	topicPrefix string
}

// Config for the MQTT subscriber.
type Config struct {
	BrokerURL   string
	Username    string
	Password    string
	ClientID    string
	TopicPrefix string
}

// New connects to the broker and returns a Subscriber (not yet subscribed).
func New(cfg Config, svc *service.ModuleService) (*Subscriber, error) {
	clientID := cfg.ClientID
	if clientID == "" {
		clientID = "module-svc"
	}
	clientID = fmt.Sprintf("%s-%d", clientID, time.Now().UnixNano()%1000000)

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetCleanSession(false)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}

	s := &Subscriber{svc: svc, topicPrefix: cfg.TopicPrefix}

	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Println("[mqtt] connected to broker")
		s.subscribe()
	})
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		log.Printf("[mqtt] connection lost: %v", err)
	})

	s.client = mqtt.NewClient(opts)
	log.Printf("[mqtt] connecting to broker %s (prefix=%q)...", cfg.BrokerURL, cfg.TopicPrefix)
	s.client.Connect()

	// Observability: with auto-reconnect the client retries silently, so warn
	// periodically until the first successful connection is established.
	go func() {
		for i := 0; i < 12; i++ {
			time.Sleep(10 * time.Second)
			if s.client.IsConnected() {
				return
			}
			log.Printf("[mqtt] still not connected to %s — retrying (broker down/unreachable?)", cfg.BrokerURL)
		}
	}()
	return s, nil
}

func (s *Subscriber) subscribe() {
	// Subscribe to the whole prefix so we can both run onboarding AND stream
	// every per-node payload (telemetry/actuator/diagnostics/alert/...) to the
	// live monitor hub. A single handler routes each message.
	allTopic := s.topicPrefix + "/#"
	if tok := s.client.Subscribe(allTopic, 0, s.onMessage); tok.Wait() && tok.Error() != nil {
		log.Printf("[mqtt] subscribe %s failed: %v", allTopic, tok.Error())
	} else {
		log.Printf("[mqtt] subscribed: %s", allTopic)
	}
}

// onMessage fans every per-node MQTT payload out to NATS (for the WS-Gateway to
// stream to the dashboard), then routes the onboarding-relevant ones
// (discovery, status) to the service.
func (s *Subscriber) onMessage(_ mqtt.Client, m mqtt.Message) {
	topic := m.Topic()
	payload := m.Payload()

	nodeID, _ := s.nodeIDFromTopic(topic, payload)
	if nodeID != "" {
		s.svc.TouchNode(nodeID)
		if strings.HasSuffix(topic, "/telemetry") {
			s.svc.PublishLive(nodeID, topic, payload)
		}
	}

	switch {
	case strings.HasSuffix(topic, "/discovery"):
		s.onDiscovery(nil, m)
	case strings.Contains(topic, "/status/"):
		s.onStatus(nil, m)
	case strings.HasSuffix(topic, "/telemetry") && nodeID != "":
		s.svc.IngestTelemetry(context.Background(), nodeID, payload)
	}
}

// nodeIDFromTopic extracts the node id from any per-node firmware topic:
//
//	{prefix}/{node_id}/telemetry|diagnostics|alert|confirm
//	{prefix}/actuator/{node_id}
//	{prefix}/status/{node_id}
//	{prefix}/discovery            -> node id comes from the payload
func (s *Subscriber) nodeIDFromTopic(topic string, payload []byte) (string, bool) {
	rel := strings.TrimPrefix(topic, s.topicPrefix+"/")
	if rel == topic {
		return "", false
	}
	parts := strings.Split(rel, "/")
	switch parts[0] {
	case "actuator", "status":
		if len(parts) > 1 && parts[1] != "" {
			return parts[1], true
		}
	case "discovery":
		var d model.DiscoveryMessage
		if json.Unmarshal(payload, &d) == nil && d.NodeID != "" {
			return d.NodeID, true
		}
	default:
		if parts[0] != "" {
			return parts[0], true
		}
	}
	return "", false
}

func (s *Subscriber) onDiscovery(_ mqtt.Client, m mqtt.Message) {
	var msg model.DiscoveryMessage
	if err := json.Unmarshal(m.Payload(), &msg); err != nil {
		log.Printf("[mqtt] bad discovery payload: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.svc.HandleDiscovery(ctx, msg); err != nil {
		log.Printf("[mqtt] handle discovery (%s): %v", msg.NodeID, err)
		return
	}
	log.Printf("[mqtt] discovery: node=%s ip=%s fw=%s", msg.NodeID, msg.IP, msg.FWVersion)
}

func (s *Subscriber) onStatus(_ mqtt.Client, m mqtt.Message) {
	// topic: {prefix}/status/{node_id}
	parts := strings.Split(m.Topic(), "/")
	nodeID := parts[len(parts)-1]

	var msg model.StatusMessage
	if err := json.Unmarshal(m.Payload(), &msg); err != nil {
		log.Printf("[mqtt] bad status payload for %s: %v", nodeID, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.svc.HandleStatus(ctx, nodeID, msg); err != nil {
		log.Printf("[mqtt] handle status (%s): %v", nodeID, err)
		return
	}
	log.Printf("[mqtt] status: node=%s -> %s", nodeID, msg.Status)
}

func (s *Subscriber) Disconnect() {
	if s.client != nil && s.client.IsConnected() {
		s.client.Disconnect(500)
	}
}
