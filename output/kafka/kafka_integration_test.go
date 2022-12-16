// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package kafka

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	//"context"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	//"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/beats/v7/libbeat/common/fmtstr"
	//"github.com/elastic/beats/v7/libbeat/outputs"
	_ "github.com/elastic/beats/v7/libbeat/outputs/codec/format"
	_ "github.com/elastic/beats/v7/libbeat/outputs/codec/json"
	//"github.com/elastic/beats/v7/libbeat/outputs/outest"
	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-shipper-client/pkg/helpers"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	//"github.com/elastic/elastic-agent-libs/mapstr"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	//codec := `"%{[message]}"`

	tests := []struct {
		title  string
		config string
		topic  string
		events  []*messages.Event
	}{
//		{
//			"publish single event to test topic",
//			fmt.Sprintf(`
//hosts: localhost:9094
//topic: %v
//`, topic),
//			topic,
//			createEvent(map[string]interface{}{
//				"host":    "localhost",
//				"message": id,
//			}),
//		},
//		{
//			"publish single event to test topic with ssl",
//						fmt.Sprintf(`
//hosts: localhost:9093
//username: beats
//password: KafkaTest
//topic: %v
//protocol: https
//sasl.mechanism: SCRAM-SHA-512
//ssl.verification_mode: certificate
//ssl.certificate_authorities: /Users/robbavey/code/elastic-agent-shipper/testing/environment/docker/dockerfiles/kafka/certs/ca-cert
//`, topic),
//			topic,
//			createEvent(map[string]interface{}{
//				"host":    "localhost",
//				"message": id,
//			}),
//		},
//		{
//			"publish single event with topic from type",
//`
//hosts: ["localhost:9094"]
//timeout: "1s"
//topic: "%{[type]}"
//`,
//			topic,
//			createEvent(map[string]interface{}{
//					"type":    topic,
//					"message": id,
//				}),
//		},
//		{
//			"publish single event to test topic using explicit codec",
//			fmt.Sprintf(`
//hosts: localhost:9094
//topic: %v
//codec.format.string: %v
//`, topic, codec),
//			topic,
//			createEvent(map[string]interface{}{
//				"host":    "localhost",
//				"message": id,
//			}),
//		},
		{
			// warning: this test uses random keys. In case keys are reused, test might fail.
			"batch publish with fields hash partitioner",
			fmt.Sprintf(`
hosts: localhost:9094
topic: %v
partition.hash.hash: ["@timestamp", "type", "message"]
`, topic),
			topic,
			createEvents(map[string]interface{}{
				"host":    "localhost",
				"message": id,
				"type": "log",
			}, 5),
		},

	}

	for i, test := range tests {
		test := test
		name := fmt.Sprintf("run test(%v): %v", i, test.title)
		fmt.Println(test.config)
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
			fmt.Println(stored)
			// validate messages
			//assert.Equal(t, len(stored), len(expected))
			assert.Equal(t, 4, len(expected))
			if len(expected) != len(stored) {
				assert.Equal(t, len(stored), len(expected))
				return
			}

			//cfgHeaders, headersSet := test.config["headers"]

			validate := validateJSON
			fmt.Println(config.Codec)
			re := regexp.MustCompile("codec.format.string: (?P<fmt>.)")
			matches := re.FindStringSubmatch(test.config)
			names := re.SubexpNames()
			data := map[string]string{}
			for i, match := range matches {
				data[names[i]] = match
			}

			//match := re.FindStringSubmatch(test.config)
			//result := make(map[string]string)
			//fmt := data["fmt"]
			if fmt, exists := data["fmt"]; exists {
				validate = makeValidateFmtStr(fmt)
			}


			seenMsgs := map[string]struct{}{}
			for _, s := range stored {
				//if headersSet {
				//	expectedHeaders, ok := cfgHeaders.([]map[string]string)
				//	assert.True(t, ok)
				//	assert.Len(t, s.Headers, len(expectedHeaders))
				//	for i, h := range s.Headers {
				//		expectedHeader := expectedHeaders[i]
				//		key := string(h.Key)
				//		value := string(h.Value)
				//		assert.Equal(t, expectedHeader["key"], key)
				//		assert.Equal(t, expectedHeader["value"], value)
				//	}
				//}
				msg := validate(t, s.Value, expected)


				seenMsgs[msg] = struct{}{}
			}
			//assert.Equal(t, len(expected), len(seenMsgs))
		})
	}
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
		fmt.Printf("string to validate is %v", msg)
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

func strDefault(a, defaults string) string {
	if len(a) == 0 {
		return defaults
	}
	return a
}

func getenv(name, defaultValue string) string {
	return strDefault(os.Getenv(name), defaultValue)
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
	fmt.Println(partitions)
	done := make(chan struct{})
	msgs := make(chan *sarama.ConsumerMessage)
	for _, partition := range partitions {
		offset := testTopicOffsets.GetOffset(topic, partition)
		partitionConsumer, err := consumer.ConsumePartition(topic, partition, offset)
		fmt.Println("Creating partition consumer", partitionConsumer)
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
					fmt.Println("IN GO FUNC FOR ", partition)

					msgs <- msg
				case <-done:
					break
				}
			}
		}(partition, partitionConsumer)
	}
	//
	var messages []*sarama.ConsumerMessage
	timer := time.After(timeout)

	for len(messages) < nMessages {
		select {
		case msg := <-msgs:
			messages = append(messages, msg)
		case <- timer:
			break
		}
	}
	assert.Equal(t, nMessages, len(messages))
	close(done)
	return messages
}


//func randMulti(batches, n int, event mapstr.M) []eventInfo {
//	var out []eventInfo
//	for i := 0; i < batches; i++ {
//		var data []beat.Event
//		for j := 0; j < n; j++ {
//			tmp := mapstr.M{}
//			for k, v := range event {
//				tmp[k] = v
//			}
//			tmp["message"] = randString(100)
//			data = append(data, beat.Event{Timestamp: time.Now(), Fields: tmp})
//		}
//
//		out = append(out, eventInfo{data})
//	}
//	return out
//}
//
//
func randString(length int) string {
	return string(randASCIIBytes(length))
}

func randASCIIBytes(length int) []byte {
	b := make([]byte, length)
	for i := range b {
		b[i] = randChar()
	}
	return b
}

func randChar() byte {
	start, end := 'a', 'z'
	if rand.Int31n(2) == 1 {
		start, end = 'A', 'Z'
	}
	return byte(rand.Int31n(end-start+1) + start)
}

func createEvent(v map[string]interface{}) []*messages.Event {
	return createEvents(v, 1)
}

func createEvents(v map[string]interface{}, count int) []*messages.Event {
	events := make([]*messages.Event, count)
	for i := 0; i < count; i++ {
		v["count"] = i
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



