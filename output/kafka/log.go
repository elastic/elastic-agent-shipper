// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package kafka

import (
	"github.com/Shopify/sarama"

	"github.com/elastic/elastic-agent-libs/logp"
)

type kafkaLogger struct {
	log *logp.Logger
}

func (kl kafkaLogger) Print(v ...interface{}) {
	kl.Log("kafka message: %v", v...)
}

func (kl kafkaLogger) Printf(format string, v ...interface{}) {
	kl.Log(format, v...)
}

func (kl kafkaLogger) Println(v ...interface{}) {
	kl.Log("kafka message: %v", v...)
}

func (kl kafkaLogger) Log(format string, v ...interface{}) {
	warn := false
	for _, val := range v {
		if err, ok := val.(sarama.KError); ok {
			if err != sarama.ErrNoError {
				warn = true
				break
			}
		}
	}
	if kl.log == nil {
		kl.log = logp.NewLogger(logSelector)
	}
	if warn {
		kl.log.Warnf(format, v...)
	} else {
		kl.log.Infof(format, v...)
	}
}
