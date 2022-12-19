// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.
//go:build integration

package kafka

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/elastic/beats/v7/libbeat/common/fmtstr"
	_ "github.com/elastic/beats/v7/libbeat/outputs/codec/format"
	_ "github.com/elastic/beats/v7/libbeat/outputs/codec/json"
	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper-client/pkg/helpers"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
)

const (
	kafkaDefaultHost     = "localhost"
	kafkaDefaultPort     = "9094"
	kafkaDefaultSASLPort = "9093"
)

func TestKafkaPublish(t *testing.T) {
	logp.TestingSetup(logp.WithSelectors("kafka"))

	id := strconv.Itoa(rand.New(rand.NewSource(int64(time.Now().Nanosecond()))).Int())
	topic := fmt.Sprintf("test-shipper-%s", id)
	codec := `"%{[message]}"`

	tests := []struct {
		title  string
		config string
		topic  string
		events []*messages.Event
	}{
		{
			"publish single event to test topic",
			fmt.Sprintf(`
hosts: %v
topic: %v
`, getTestKafkaHost(), topic),
			topic,
			createEvent(map[string]interface{}{
				"host": "localhost",
			}, id),
		},
		{
			"publish single event with topic from type",
			fmt.Sprintf(`
hosts: %v
timeout: "1s"
topic: %v
`, getTestKafkaHost(), `"%{[type]}"`),
			topic,
			createEvent(map[string]interface{}{
				"host": getTestKafkaHost(),
				"type": topic,
			}, id),
		},
		{
			"publish single event to test topic using explicit codec",
			fmt.Sprintf(`
hosts: %v
topic: %v
codec.format.string: %v
`, getTestKafkaHost(), topic, codec),
			topic,
			createEvent(map[string]interface{}{
				"host": getTestKafkaHost(),
			}, id),
		},
		{
			"publish batch of event to test topic",
			fmt.Sprintf(`
hosts: %v
topic: %v
`, getTestKafkaHost(), topic),
			topic,
			createEvents(map[string]interface{}{
				"host": "localhost",
			}, id, 10),
		},
		{
			"publish batch of events with topic from type",
			fmt.Sprintf(`
hosts: %v
timeout: "1s"
topic: %v
`, getTestKafkaHost(), `"%{[type]}"`),
			topic,
			createEvents(map[string]interface{}{
				"host": getTestKafkaHost(),
				"type": topic,
			}, id, 10),
		},
		{
			"batch publish with headers",
			fmt.Sprintf(`
hosts: %v
topic: %v
headers:
  - key: "some-key"
    value: "some value"
  - key: "another-key"
    value: "another value"
`, getTestKafkaHost(), topic),
			topic,
			createEvents(map[string]interface{}{
				"host": getTestKafkaHost(),
				"type": "log",
			}, id, 5),
		},
		{
			"batch publish with random partitioner",
			fmt.Sprintf(`
hosts: %v
topic: %v
partition.random:
  group_events: 1
`, getTestKafkaHost(), topic),
			topic,
			createEvents(map[string]interface{}{
				"host": getTestKafkaHost(),
				"type": "log",
			}, id, 5),
		},
		{
			"batch publish with fields hash partitioner",
			fmt.Sprintf(`
hosts: %v
topic: %v
partition.hash.hash: ["@timestamp", "type", "message"]
`, getTestKafkaHost(), topic),
			topic,
			createEvents(map[string]interface{}{
				"host": getTestKafkaHost(),
				"type": "log",
			}, id, 5),
		},
		{
			"batch publish with hash partitioner with key",
			fmt.Sprintf(`
hosts: %v
topic: %v
key: %v
partition.hash:
`, getTestKafkaHost(), topic, `"%{[message]}"`),
			topic,
			createEvents(map[string]interface{}{
				"host": getTestKafkaHost(),
				"type": "log",
			}, id, 5),
		},
		{
			"batch publish with hash partitioner without key (fallback to random)",
			fmt.Sprintf(`
hosts: %v
topic: %v
partition.hash:
`, getTestKafkaHost(), topic),
			topic,
			createEvents(map[string]interface{}{
				"host": getTestKafkaHost(),
				"type": "log",
			}, id, 5),
		},

		{
			"publish single event to test topic with ssl",
			fmt.Sprintf(`
hosts: %v
username: beats
password: KafkaTest
topic: %v
protocol: https
sasl.mechanism: SCRAM-SHA-512
ssl.verification_mode: certificate
ssl.certificate_authorities: ../../testing/environment/docker/dockerfiles/kafka/certs/ca-cert
`, getTestSASLKafkaHost(), topic),
			topic,
			createEvent(map[string]interface{}{
				"host": getTestSASLKafkaHost(),
			}, id),
		},
	}

	for i, test := range tests {
		name := fmt.Sprintf("run test(%v): %v", i, test.title)
		cfg, err := config.NewConfigWithYAML([]byte(test.config), "")
		require.NoErrorf(t, err, "%s: error making config from yaml: %s, %s", name, test.config, err)
		config := DefaultConfig()
		err = cfg.Unpack(&config)
		t.Run(name, func(t *testing.T) {
			client, err := makeKafka(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Connect(); err != nil {
				t.Fatal(err)
			}
			defer client.Close()

			_, err = client.publishEvents(test.events)
			expected := test.events

			timeout := 10 * time.Second
			stored := testReadFromKafkaTopic(t, test.topic, len(expected), timeout)
			// validate messages
			assert.Equal(t, len(stored), len(expected))
			if len(expected) != len(stored) {
				assert.Equal(t, len(stored), len(expected))
				return
			}

			validate := validateJSON

			re := regexp.MustCompile("codec.format.string: (?P<fmt>.)")
			matches := re.FindStringSubmatch(test.config)
			names := re.SubexpNames()
			data := map[string]string{}
			for i, match := range matches {
				data[names[i]] = match
			}

			if fmt, exists := data["fmt"]; exists {
				validate = makeValidateFmtStr(fmt)
			}

			seenMsgs := map[string]struct{}{}
			headers := config.Headers

			for _, s := range stored {
				if headers != nil {
					assert.Len(t, s.Headers, len(headers))
					for i, h := range s.Headers {
						expectedHeader := headers[i]
						key := string(h.Key)
						value := string(h.Value)
						assert.Equal(t, expectedHeader.Key, key)
						assert.Equal(t, expectedHeader.Value, value)
					}
				}
				msg := validate(t, s.Value, expected)
				seenMsgs[msg] = struct{}{}
			}
			assert.Equal(t, len(expected), len(seenMsgs))
		})
	}
}

func getTestKafkaHost() string {
	return fmt.Sprintf("%v:%v",
		getenv("KAFKA_HOST", kafkaDefaultHost),
		getenv("KAFKA_PORT", kafkaDefaultPort),
	)
}

func getTestSASLKafkaHost() string {
	return fmt.Sprintf("%v:%v",
		getenv("KAFKA_HOST", kafkaDefaultHost),
		getenv("KAFKA_SASL_PORT", kafkaDefaultSASLPort),
	)
}

func validateJSON(t *testing.T, value []byte, events []*messages.Event) string {
	var decoded map[string]interface{}
	err := json.Unmarshal(value, &decoded)
	if err != nil {
		t.Errorf("can not json decode event value: %v", value)
		return ""
	}

	msg := decoded["message"].(string)
	event := findEvent(events, msg)
	if event == nil {
		t.Errorf("could not find expected event with message: %v", msg)
		return ""
	}

	assert.Equal(t, decoded["type"], mapstrForStruct(event.GetFields())["type"])

	return msg
}

func makeValidateFmtStr(fmtStr string) func(*testing.T, []byte, []*messages.Event) string {
	fmtString := fmtstr.MustCompileEvent(fmtStr)
	return func(t *testing.T, value []byte, events []*messages.Event) string {
		msg := string(value)
		event := findEvent(events, msg)
		if event == nil {
			t.Errorf("could not find expected event with message: %v", msg)
			return ""
		}
		beatsEvent := beatsEventForProto(event)
		_, err := fmtString.Run(beatsEvent)
		if err != nil {
			t.Fatal(err)
		}
		return msg
	}
}

func findEvent(events []*messages.Event, msg interface{}) *messages.Event {
	for _, e := range events {
		fields := mapstrForStruct(e.GetFields())
		if fields["message"] == msg {
			return e
		}
	}
	return nil
}

func createEvent(v map[string]interface{}, id string) []*messages.Event {
	return createEvents(v, id, 1)
}

func createEvents(v map[string]interface{}, id string, count int) []*messages.Event {
	events := make([]*messages.Event, count)
	for i := 0; i < count; i++ {
		v["message"] = fmt.Sprintf("%v:%v", id, i)
		fields, err := helpers.NewStruct(v)
		if err != nil {
			return nil
		}

		e := &messages.Event{
			Timestamp: timestamppb.Now(),
			Source: &messages.Source{
				InputId:  "input",
				StreamId: "stream",
			},
			DataStream: &messages.DataStream{
				Type:      "log",
				Dataset:   "default",
				Namespace: "default",
			},
			Metadata: fields,
			Fields:   fields,
		}
		events[i] = e
	}
	return events
}

var testTopicOffsets = topicOffsetMap{}

func testReadFromKafkaTopic(
	t *testing.T, topic string, nMessages int,
	timeout time.Duration,
) []*sarama.ConsumerMessage {
	consumer := newTestConsumer(t)
	defer func() {
		consumer.Close()
	}()

	partitions, err := consumer.Partitions(topic)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	msgs := make(chan *sarama.ConsumerMessage)
	for _, partition := range partitions {
		offset := testTopicOffsets.GetOffset(topic, partition)
		partitionConsumer, err := consumer.ConsumePartition(topic, partition, offset)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			partitionConsumer.Close()
		}()

		go func(p int32, pc sarama.PartitionConsumer) {
			for {
				select {
				case msg, ok := <-pc.Messages():
					if !ok {
						break
					}
					testTopicOffsets.SetOffset(topic, p, msg.Offset+1)
					msgs <- msg
				case <-done:
					break
				}
			}
		}(partition, partitionConsumer)
	}

	var messages []*sarama.ConsumerMessage
	timer := time.After(timeout)

	for len(messages) < nMessages {
		select {
		case msg := <-msgs:
			messages = append(messages, msg)
		case <-timer:
			break
		}
	}
	assert.Equal(t, nMessages, len(messages))
	close(done)
	return messages
}

func strDefault(a, defaults string) string {
	if len(a) == 0 {
		return defaults
	}
	return a
}

func getenv(name, defaultValue string) string {
	return strDefault(os.Getenv(name), defaultValue)
}

func newTestConsumer(t *testing.T) sarama.Consumer {
	hosts := []string{getTestKafkaHost()}
	consumer, err := sarama.NewConsumer(hosts, nil)
	if err != nil {
		t.Fatal(err)
	}
	return consumer
}

// topicOffsetMap is threadsafe map from topic => partition => offset
type topicOffsetMap struct {
	m  map[string]map[int32]int64
	mu sync.RWMutex
}

func (m *topicOffsetMap) GetOffset(topic string, partition int32) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.m == nil {
		return sarama.OffsetOldest
	}

	topicMap, ok := m.m[topic]
	if !ok {
		return sarama.OffsetOldest
	}

	offset, ok := topicMap[partition]
	if !ok {
		return sarama.OffsetOldest
	}

	return offset
}

func (m *topicOffsetMap) SetOffset(topic string, partition int32, offset int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.m == nil {
		m.m = map[string]map[int32]int64{}
	}

	if _, ok := m.m[topic]; !ok {
		m.m[topic] = map[int32]int64{}
	}

	m.m[topic][partition] = offset
}
