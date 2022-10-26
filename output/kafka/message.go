// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package kafka

import (
	"time"
	"fmt"
	"github.com/Shopify/sarama"

	"github.com/elastic/beats/v7/libbeat/beat"
)

type Message struct {
	msg sarama.ProducerMessage

	topic string
	key   []byte
	value []byte
	ref   *MsgRef
	ts    time.Time

	hash      uint32
	partition int32

	//data messages.Event
	data beat.Event
}

var kafkaMessageKey interface{} = int(0)

func (m *Message) initProducerMessage() {
	//fmt.Println("Sending metadata %s to topic %s", m, m.topic)
	fmt.Printf("Sending key %s to topic %s on partition %s\n", m.key, m.topic, m.partition)

	m.msg = sarama.ProducerMessage{
		Metadata:  m,
		Topic:     m.topic,
		Key:       sarama.ByteEncoder(m.key),
		Value:     sarama.ByteEncoder(m.value),
		Timestamp: m.ts,
	}

	if m.ref != nil {
		m.msg.Headers = m.ref.client.recordHeaders
	}
}
