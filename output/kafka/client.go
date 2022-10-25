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
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	//"encoding/json"
	//"google.golang.org/protobuf/encoding/protojson"

	"github.com/Shopify/sarama"
	"github.com/eapache/go-resiliency/breaker"
	"github.com/elastic/beats/v7/libbeat/beat"
	//"github.com/elastic/beats/v7/libbeat/beat/events"


	"github.com/elastic/beats/v7/libbeat/common/fmtstr"
	//"github.com/elastic/beats/v7/libbeat/outputs"
	"github.com/elastic/beats/v7/libbeat/outputs/codec"
	//"github.com/elastic/beats/v7/libbeat/outputs/outil"
	//"github.com/elastic/beats/v7/libbeat/publisher"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/testing"
	"github.com/elastic/elastic-agent-libs/transport"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
	//"go.elastic.co/apm"

)

type Client struct {
	log      *logp.Logger
	//observer outputs.Observer         TODO: Figure out how to deal with
	hosts    []string
	//topic    outil.Selector
	topic string
	key      *fmtstr.EventFormatString
	index    string
	codec    codec.Codec

	//topics    outil.Selector            //       TODO: Figure out how to do event interpolation to determine topic from event contents
	//key      string            //       TODO: Figure out how to do event interpolation to determine key from event contents
	//index    string                       TODO: This looks like it is used to populate metadata?
	//codec    codec.Codec RWB what do we do with codecs
	config   sarama.Config
	mux      sync.Mutex
	done     chan struct{}

	producer sarama.AsyncProducer
	recordHeaders []sarama.RecordHeader
	wg sync.WaitGroup
}

type MsgRef struct {
	client *Client
	count  int32
	total  int
	failed []beat.Event
	//batch  publisher.Batch            // TODO: Let's figure out what this means

	err error
}

var (
	errNoTopicsSelected = errors.New("no topic could be selected")
)

func newKafkaClient(
	//observer outputs.Observer,            TODO: No need for observer, AFAICT
	hosts []string,
	index string,            //             TODO: As above, let's figure out if we still need this. Maybe used for  event metadata?
	key      *fmtstr.EventFormatString,
	//key string,    //                        TODO: generate key name from event contents
	topic string, //                        TODO: generate topic name from event contents
	//topic    outil.Selector,

	//index string,       //                  TODO: As above, let's figure out if we still need this. Maybe used for  event metadata?
	headers []Header,
	writer codec.Codec,      //             TODO: Figure out what to do with event serialization
	cfg *sarama.Config,
) (*Client, error) {
	c := &Client{
		log:      logp.NewLogger(logSelector),
		//observer: observer,               TODO: Observer removal
		hosts:    hosts,
		topic:    topic,
		key:      key,
		index:    strings.ToLower(index), //TODO: Figure out what to do with index
		codec:    writer,    //             TODO: Figure out what to do with event serialization
		config:   *cfg,
		done:     make(chan struct{}),
	}

	if len(headers) != 0 {
		recordHeaders := make([]sarama.RecordHeader, 0, len(headers))
		for _, h := range headers {
			if h.Key == "" {
				continue
			}
			recordHeader := sarama.RecordHeader{
				Key:   []byte(h.Key),
				Value: []byte(h.Value),
			}

			recordHeaders = append(recordHeaders, recordHeader)
		}
		c.recordHeaders = recordHeaders
	}

	return c, nil
}

func (c *Client) Connect() error {
	fmt.Println("Creating producer! %s\n", c.hosts)
	c.mux.Lock()
	defer c.mux.Unlock()

	c.log.Debugf("connect: %v", c.hosts)

	// try to connect
	producer, err := sarama.NewAsyncProducer(c.hosts, &c.config)
	if err != nil {
		c.log.Errorf("Kafka connect fails with: %+v", err)
		return err
	}
	fmt.Println("Created producer! %s\n", producer)
	c.producer = producer

	c.wg.Add(2)
	// TODO: Figure out how to indicate success and failure so we can ack and retry.
	go c.successWorker(producer.Successes())
	go c.errorWorker(producer.Errors())

	return nil
}

func (c *Client) Close() error {
	c.mux.Lock()
	defer c.mux.Unlock()
	c.log.Debug("closed kafka client")

	// producer was not created before the close() was called.
	if c.producer == nil {
		return nil
	}

	close(c.done)
	c.producer.AsyncClose()
	c.wg.Wait()
	c.producer = nil
	return nil
}

// PublishEvents sends all events to kafka. On error a slice with all
// events not published or confirmed to be processed by kafka will be
// returned. The input slice backing memory will be reused by return the value.
func (client *Client) publishEvents(ctx context.Context, data []*messages.Event) ([]*messages.Event, error) {

	// TODO: APM integration
	//span, ctx := apm.StartSpan(ctx, "publishEvents", "output")
	//defer span.End()
	//begin := time.Now()
	fmt.Printf("working with client %s", client)
	//st := client.observer


	ref := &MsgRef{
		client: client,
		count:  int32(len(data)),
		total:  len(data),
		failed: nil,
		//batch:  batch,
	}

	// TODO: Deal with observer tracking
	//if st != nil {
	//	st.NewBatch(len(data))
	//}

	if len(data) == 0 {
		return nil, nil
	}

	//origCount := len(data)
	//span.Context.SetLabel("events_original", origCount)

	ch := client.producer.Input()
	for i := range data {
		//d := data[i]
		d := beatsEventForProto(data[i])
		fmt.Println("Creating event message\n")
		msg, err := client.getEventMessage(d)
		//fmt.Println("Created event message %s", msg)
		if err != nil {
			client.log.Errorf("Dropping event: %+v", err)
			ref.done()
			// TODO: Metrics
			//client.observer.Dropped(1)
			continue
		}

		msg.ref = ref
		msg.initProducerMessage()
		fmt.Println("Sending message to producer")
		ch <- &msg.msg
	}


	// TODO: Return properly
	// Need to figure out a way to track the completion status of events and return these to the shipper, handling
	// retries, etc

	return nil, nil
}


func (c *Client) String() string {
	return "kafka(" + strings.Join(c.hosts, ",") + ")"
}

//func (c *Client) getEventMessage(data *messages.Event) (*Message, error) {
func (c *Client) getEventMessage(data *beat.Event) (*Message, error) {
	msg := &Message{partition: -1, data: *data}

	// TODO: As fas as I can tell, this snippet of code is required to mark the topic and partition
	// on an event, so that in the event of a retry, the partition and topic is no longer required to be re-calculated
	// as stability is expected over retry.

	// RWB partition, topic and value are set on the event cache...
	//value, err := data.Cache.GetValue("partition")
	//if err == nil {
	//	if c.log.IsDebug() {
	//		c.log.Debugf("got event.Meta[\"partition\"] = %v", value)
	//	}
	//	if partition, ok := value.(int32); ok {
	//		msg.partition = partition
	//	}
	//}

	//value, err = data.Cache.GetValue("topic")
	//if err == nil {
	//	if c.log.IsDebug() {
	//		c.log.Debugf("got event.Meta[\"topic\"] = %v", value)
	//	}
	//	if topic, ok := value.(string); ok {
	//		msg.topic = topic
	//	}
	//}


	//TODO: Topic creation based on event interpolation
	//if msg.topic == "" {
	//	topic, err := c.topic.Select(data)
	//
	//	if err != nil {
	//		return nil, fmt.Errorf("setting kafka topic failed with %v", err)
	//	}
	//	if topic == "" {
	//		return nil, errNoTopicsSelected
	//	}
	//	msg.topic = topic
	//	//if _, err := data.Cache.Put("topic", topic); err != nil {
	//	//	return nil, fmt.Errorf("setting kafka topic in publisher event failed: %v", err)
	//	//}
	//}

	msg.topic = c.topic

	//// TODO: This is some homemade serialization which is missing a bunch of features from where we want to be,
	//// and serializes in a pretty ugly format, which exposes the protobuf internal structure.
	//// We need to translate this more effectively, and commonly between outputs.
	//
	//serializedEvent, err := protojson.Marshal(data)

	//fmt.Println("original data %s\n", data)
	//fmt.Println("Sending event %s\n", string(serializedEvent))

	//c.codec.Encode("c.index", event)
	//if err != nil {
	//	fmt.Println("Unable to send event")
	//	if c.log.IsDebug() {
	//		c.log.Debugf("failed event: %v", data)
	//	}
	//	return nil, err
	//}
	//

	serializedEvent, err := c.codec.Encode(c.index, data)
	if err != nil {
		if c.log.IsDebug() {
			c.log.Debugf("failed event: %v", data)
		}
		return nil, err
	}

	buf := make([]byte, len(serializedEvent))
	copy(buf, serializedEvent)
	msg.value = buf

	//msg.value = serializedEvent

	//fmt.Println("The fields are %s\n", data.Fields)
	// message timestamps have been added to kafka with version 0.10.0.0
    // TODO: Figure out timestamp conversion
	if c.config.Version.IsAtLeast(sarama.V0_10_0_0) {
		//msg.ts = data.Timestamp.AsTime()
		msg.ts = data.Timestamp
	}
	// TODO: Figure out keys from event contents.
	//msg.key = []byte(c.key)
	if c.key != nil {
		if key, err := c.key.RunBytes(data); err == nil {
			msg.key = key
		}
	}

	return msg, nil
}

func (c *Client) successWorker(ch <-chan *sarama.ProducerMessage) {
	defer c.wg.Done()
	defer c.log.Debug("Stop kafka ack worker")

	for libMsg := range ch {
		msg := libMsg.Metadata.(*Message)
		msg.ref.done()
	}
}

func (c *Client) errorWorker(ch <-chan *sarama.ProducerError) {
	breakerOpen := false
	defer c.wg.Done()
	defer c.log.Debug("Stop kafka error handler")

	for errMsg := range ch {
		msg := errMsg.Msg.Metadata.(*Message)
		msg.ref.fail(msg, errMsg.Err)

		// TODO: Understand the error breaker, and how it maps to the new model.
		if errMsg.Err == breaker.ErrBreakerOpen {
			// ErrBreakerOpen is a very special case in Sarama. It happens only when
			// there have been repeated critical (broker / topic-level) errors, and it
			// puts Sarama into a state where it immediately rejects all input
			// for 10 seconds, ignoring retry / backoff settings.
			// With this output's current design (in which Publish passes through to
			// Sarama's input channel with no further synchronization), retrying
			// these failed values causes an infinite retry loop that degrades
			// the entire system.
			// "Nice" approaches and why we haven't used them:
			// - Use exposed API to navigate this state and its effect on retries.
			//   * Unfortunately, Sarama's circuit breaker and its errors are
			//     hard-coded and undocumented. We'd like to address this in the
			//     future.
			// - If a batch fails with a circuit breaker error, delay before
			//   retrying it.
			//   * This would fix the most urgent performance issues, but requires
			//     extra bookkeeping because the Kafka output handles each batch
			//     independently. It results in potentially many batches / 10s of
			//     thousands of events being loaded and attempted, even though we
			//     know there's a fatal error early in the first batch. It also
			//     makes it hard to know when each batch should be retried.
			// - In the Kafka Publish method, add a blocking first-pass intake step
			//   that can gate on error conditions, rather than handing off data
			//   to Sarama immediately.
			//   * This would fix the issue but would require a lot of work and
			//     testing, and we need a fix for the release now. It's also a
			//     fairly elaborate workaround for something that might be
			//     easier to fix in the library itself.
			//
			// Instead, we have applied the following fix, which is not very "nice"
			// but satisfies all other important constraints:
			// - When we receive a circuit breaker error, sleep for 10 seconds
			//   (Sarama's hard-coded timeout) on the _error worker thread_.
			//
			// This works because connection-level errors that can trigger the
			// circuit breaker are on the critical path for input processing, and
			// thus blocking on the error channel applies back-pressure to the
			// input channel. This means that if there are any more errors while the
			// error worker is asleep, any call to Publish will block until we
			// start reading again.
			//
			// Reasons this solution is preferred:
			// - It responds immediately to Sarama's global error state, rather than
			//   trying to detect it independently in each batch or adding more
			//   cumbersome synchronization to the output
			// - It gives the minimal delay that is consistent with Sarama's
			//   internal behavior
			// - It requires only a few lines of code and no design changes
			//
			// That said, this is still relying on undocumented library internals
			// for correct behavior, which isn't ideal, but the error itself is an
			// undocumented library internal, so this is de facto necessary for now.
			// We'd like to have a more official / permanent fix merged into Sarama
			// itself in the future.

			// The "breakerOpen" flag keeps us from sleeping the first time we see
			// a circuit breaker error, because it might be an old error still
			// sitting in the channel from 10 seconds ago. So we only end up
			// sleeping every _other_ reported breaker error.
			if breakerOpen {
				// Immediately log the error that presumably caused this state,
				// since the error reporting on this batch will be delayed.
				if msg.ref.err != nil {
					fmt.Println("Kafka (topic=%v): %v", msg.topic, msg.ref.err)
					c.log.Errorf("Kafka (topic=%v): %v", msg.topic, msg.ref.err)
				}
				select {
				case <-time.After(10 * time.Second):
					// Sarama's circuit breaker is hard-coded to reject all inputs
					// for 10sec.
				case <-msg.ref.client.done:
					// Allow early bailout if the output itself is closing.
				}
				breakerOpen = false
			} else {
				breakerOpen = true
			}
		}
	}
}

func (r *MsgRef) done() {
	r.dec()
}

func (r *MsgRef) fail(msg *Message, err error) {
	fmt.Println("Sending message FAILED!!! %v", err)

	switch err {
	case sarama.ErrInvalidMessage:
		r.client.log.Errorf("Kafka (topic=%v): dropping invalid message", msg.topic)
		// TODO: Metrics, and how to represent.
		//r.client.observer.Dropped(1)

	case sarama.ErrMessageSizeTooLarge, sarama.ErrInvalidMessageSize:
		r.client.log.Errorf("Kafka (topic=%v): dropping too large message of size %v.",
			msg.topic,
			len(msg.key)+len(msg.value))
		//r.client.observer.Dropped(1)

	case breaker.ErrBreakerOpen:
		// Add this message to the failed list, but don't overwrite r.err since
		// all the breaker error means is "there were a lot of other errors".
		r.failed = append(r.failed, msg.data)

	default:
		r.failed = append(r.failed, msg.data)
		if r.err == nil {
			// Don't overwrite an existing error. This way at tne end of the batch
			// we report the first error that we saw, rather than the last one.
			r.err = err
		}
	}
	r.dec()
}

func (r *MsgRef) dec() {
	i := atomic.AddInt32(&r.count, -1)
	if i > 0 {
		return
	}

	r.client.log.Debug("finished kafka batch")
	// TODO: Metrics and how to deal with observer
	//stats := r.client.observer

	err := r.err
	if err != nil {
		failed := len(r.failed)
		fmt.Println("Failed to send %s", r.failed)
		total := r.total
		success := total - failed
        fmt.Println("Successfully sent %s", success)
		// TODO: Do we rely on kafka for retries now?
//		r.batch.RetryEvents(r.failed)

		//stats.Failed(failed)
		//if success > 0 {
		//	stats.Acked(success)
		//}

		r.client.log.Debugf("Kafka publish s with: %+v", err)
	} else {
		// TODO: Make sure we have a solid story for acking and nacking
		//r.batch.ACK()
		//stats.Acked(r.total)
		fmt.Println("Acked %s", r.total)
	}
}

func (c *Client) Test(d testing.Driver) {
	if c.config.Net.TLS.Enable == true {
		d.Warn("TLS", "Kafka output doesn't support TLS testing")
	}

	for _, host := range c.hosts {
		d.Run("Kafka: "+host, func(d testing.Driver) {
			netDialer := transport.TestNetDialer(d, c.config.Net.DialTimeout)
			_, err := netDialer.Dial("tcp", host)
			d.Error("dial up", err)
		})
	}

}
