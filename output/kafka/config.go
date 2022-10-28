// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package kafka

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/Shopify/sarama"

	"github.com/elastic/beats/v7/libbeat/common/fmtstr"
	libkafka "github.com/elastic/beats/v7/libbeat/common/kafka"
	"github.com/elastic/beats/v7/libbeat/common/transport/kerberos"
	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/monitoring"
	"github.com/elastic/elastic-agent-libs/monitoring/adapter"
	"github.com/elastic/elastic-agent-libs/transport/tlscommon"
)

type BackoffConfig struct {
	Init time.Duration `config:"init"`
	Max  time.Duration `config:"max"`
}

type Header struct {
	Key   string `config:"key"`
	Value string `config:"value"`
}

type Config struct {
	Enabled            bool                      `config:"enabled"`
	Topic              *fmtstr.EventFormatString `config:"topic"`
	Hosts              []string                  `config:"hosts"`
	TLS                *tlscommon.Config         `config:"ssl"`
	Kerberos           *kerberos.Config          `config:"kerberos"`
	Timeout            time.Duration             `config:"timeout"             validate:"min=1"`
	Metadata           MetaConfig                `config:"metadata"`
	Key                *fmtstr.EventFormatString `config:"key"`
	Partition          map[string]*config.C      `config:"partition"`
	KeepAlive          time.Duration             `config:"keep_alive"          validate:"min=0"`
	MaxMessageBytes    *int                      `config:"max_message_bytes"   validate:"min=1"`
	RequiredACKs       *int                      `config:"required_acks"       validate:"min=-1"`
	BrokerTimeout      time.Duration             `config:"broker_timeout"      validate:"min=1"`
	Compression        string                    `config:"compression"`
	CompressionLevel   int                       `config:"compression_level"`
	Version            libkafka.Version          `config:"version"`
	BulkMaxSize        int                       `config:"bulk_max_size"`
	BulkFlushFrequency time.Duration             `config:"bulk_flush_frequency"`
	MaxRetries         int                       `config:"max_retries"         validate:"min=-1,nonzero"`
	Headers            []Header                  `config:"Headers"`
	Backoff            BackoffConfig             `config:"backoff"`
	ClientID           string                    `config:"client_id"`
	ChanBufferSize     int                       `config:"channel_buffer_size" validate:"min=1"`
	Username           string                    `config:"username"`
	Password           string                    `config:"password"`
	Sasl               libkafka.SaslConfig       `config:"sasl"`
	EnableFAST         bool                      `config:"enable_krb5_fast"`
	// TODO: Figure out codecs
	//Codec              codec.Config              `config:"codec"`

}

type MetaConfig struct {
	Retry       MetaRetryConfig `config:"retry"`
	RefreshFreq time.Duration   `config:"refresh_frequency" validate:"min=0"`
	Full        bool            `config:"full"`
}

type MetaRetryConfig struct {
	Max     int           `config:"max"     validate:"min=0"`
	Backoff time.Duration `config:"backoff" validate:"min=0"`
}

var compressionModes = map[string]sarama.CompressionCodec{
	// As of sarama 1.24.1, zstd support is broken
	// (https://github.com/Shopify/sarama/issues/1252), which needs to be
	// addressed before we add support here.
	"none":   sarama.CompressionNone,
	"no":     sarama.CompressionNone,
	"off":    sarama.CompressionNone,
	"gzip":   sarama.CompressionGZIP,
	"lz4":    sarama.CompressionLZ4,
	"snappy": sarama.CompressionSnappy,
}

//const (
//	saslTypePlaintext   = sarama.SASLTypePlaintext
//	saslTypeSCRAMSHA256 = sarama.SASLTypeSCRAMSHA256
//	saslTypeSCRAMSHA512 = sarama.SASLTypeSCRAMSHA512
//)

func DefaultConfig() Config {
	return Config{
		Hosts:              nil,
		TLS:                nil,
		Kerberos:           nil,
		Timeout:            30 * time.Second,
		BulkMaxSize:        2048,
		BulkFlushFrequency: 0,
		Metadata: MetaConfig{
			Retry: MetaRetryConfig{
				Max:     3,
				Backoff: 250 * time.Millisecond,
			},
			RefreshFreq: 10 * time.Minute,
			Full:        false,
		},
		KeepAlive:        0,
		MaxMessageBytes:  nil, // use library default
		RequiredACKs:     nil, // use library default
		BrokerTimeout:    10 * time.Second,
		Compression:      "gzip",
		CompressionLevel: 4,
		Version:          libkafka.Version("1.0.0"),
		MaxRetries:       3,
		Headers:          nil,
		Backoff: BackoffConfig{
			Init: 1 * time.Second,
			Max:  60 * time.Second,
		},
		ClientID:       "elastic-agent",
		ChanBufferSize: 256,
		Username:       "",
		Password:       "",
	}
}

func (client *Config) Validate() error {
	if client.Enabled {
		if len(client.Hosts) == 0 {
			return errors.New("missing required field 'hosts'")
		}

		if client.Topic == nil {
			return errors.New("missing required field 'topic'")
		}

		if _, ok := compressionModes[strings.ToLower(client.Compression)]; !ok {
			return fmt.Errorf("compression mode '%v' unknown", client.Compression)
		}

		if err := client.Version.Validate(); err != nil {
			return err
		}

		if client.Username != "" && client.Password == "" {
			return fmt.Errorf("password must be set when username is configured")
		}

	}
	return nil
}

func newSaramaConfig(log *logp.Logger, config Config) (*sarama.Config, error) {
	partitioner, err := makePartitioner(log, config.Partition)
	if err != nil {
		return nil, err
	}

	k := sarama.NewConfig()

	// configure network level properties
	timeout := config.Timeout
	k.Net.DialTimeout = timeout
	k.Net.ReadTimeout = timeout
	k.Net.WriteTimeout = timeout
	k.Net.KeepAlive = config.KeepAlive
	k.Producer.Timeout = config.BrokerTimeout
	k.Producer.CompressionLevel = config.CompressionLevel

	tls, err := tlscommon.LoadTLSConfig(config.TLS)
	if err != nil {
		return nil, err
	}

	if tls != nil {
		k.Net.TLS.Enable = true
		k.Net.TLS.Config = tls.BuildModuleClientConfig("")
	}

	switch {
	// TODO: Are we still planning on Kerberos to be beta?
	case config.Kerberos.IsEnabled():
		//cfgwarn.Beta("Kerberos authentication for Kafka is beta.")

		// Due to a regrettable past decision, the flag controlling Kerberos
		// FAST authentication was initially added to the output configuration
		// rather than the shared Kerberos configuration. To avoid a breaking
		// change, we still check for the old flag, but it is deprecated and
		// should be removed in a future version.
		enableFAST := config.Kerberos.EnableFAST || config.EnableFAST

		k.Net.SASL.Enable = true
		k.Net.SASL.Mechanism = sarama.SASLTypeGSSAPI
		k.Net.SASL.GSSAPI = sarama.GSSAPIConfig{
			AuthType:           int(config.Kerberos.AuthType),
			KeyTabPath:         config.Kerberos.KeyTabPath,
			KerberosConfigPath: config.Kerberos.ConfigPath,
			ServiceName:        config.Kerberos.ServiceName,
			Username:           config.Kerberos.Username,
			Password:           config.Kerberos.Password,
			Realm:              config.Kerberos.Realm,
			DisablePAFXFAST:    !enableFAST,
		}

	case config.Username != "":
		k.Net.SASL.Enable = true
		k.Net.SASL.User = config.Username
		k.Net.SASL.Password = config.Password
		config.Sasl.ConfigureSarama(k)
	}

	// configure metadata update properties
	k.Metadata.Retry.Max = config.Metadata.Retry.Max
	k.Metadata.Retry.Backoff = config.Metadata.Retry.Backoff
	k.Metadata.RefreshFrequency = config.Metadata.RefreshFreq
	k.Metadata.Full = config.Metadata.Full

	// configure producer API properties
	if config.MaxMessageBytes != nil {
		k.Producer.MaxMessageBytes = *config.MaxMessageBytes
	}
	if config.RequiredACKs != nil {
		k.Producer.RequiredAcks = sarama.RequiredAcks(*config.RequiredACKs)
	}

	compressionMode, ok := compressionModes[strings.ToLower(config.Compression)]
	if !ok {
		return nil, fmt.Errorf("unknown compression mode: '%v'", config.Compression)
	}
	k.Producer.Compression = compressionMode

	k.Producer.Return.Successes = true // enable return channel for signaling
	k.Producer.Return.Errors = true

	// have retries being handled by libbeat, disable retries in sarama library
	retryMax := config.MaxRetries
	if retryMax < 0 {
		retryMax = 1000
	}
	k.Producer.Retry.Max = retryMax
	k.Producer.Retry.BackoffFunc = makeBackoffFunc(config.Backoff)

	// configure per broker go channel buffering
	k.ChannelBufferSize = config.ChanBufferSize

	// configure bulk size
	k.Producer.Flush.MaxMessages = config.BulkMaxSize
	if config.BulkFlushFrequency > 0 {
		k.Producer.Flush.Frequency = config.BulkFlushFrequency
	}

	// configure client ID
	k.ClientID = config.ClientID

	version, ok := config.Version.Get()
	if !ok {
		return nil, fmt.Errorf("unknown/unsupported kafka version: %v", config.Version)
	}
	k.Version = version

	k.Producer.Partitioner = partitioner
	k.MetricRegistry = adapter.GetGoMetrics(
		monitoring.Default,
		"libbeat.outputs.kafka", // TODO: Come up with a new name here
		adapter.Rename("incoming-byte-rate", "bytes_read"),
		adapter.Rename("outgoing-byte-rate", "bytes_write"),
		adapter.GoMetricsNilify,
	)

	if err := k.Validate(); err != nil {
		log.Errorf("Invalid kafka configuration: %+v", err)
		return nil, err
	}
	return k, nil
}

// TODO: Incorporate this

// makeBackoffFunc returns a stateless implementation of exponential-backoff-with-jitter. It is conceptually
// equivalent to the stateful implementation used by other outputs, EqualJitterBackoff.
func makeBackoffFunc(cfg BackoffConfig) func(retries, maxRetries int) time.Duration {
	maxBackoffRetries := int(math.Ceil(math.Log2(float64(cfg.Max) / float64(cfg.Init))))

	return func(retries, _ int) time.Duration {
		// compute 'base' duration for exponential backoff
		dur := cfg.Max
		if retries < maxBackoffRetries {
			dur = time.Duration(uint64(cfg.Init) * uint64(1<<retries))
		}

		// apply about equaly distributed jitter in second half of the interval, such that the wait
		// time falls into the interval [dur/2, dur]
		limit := int64(dur / 2)
		jitter := rand.Int63n(limit + 1)
		return time.Duration(limit + jitter)
	}
}