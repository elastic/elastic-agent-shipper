// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package kafka

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/elastic/beats/v7/libbeat/beat"
	libconfig "github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

func TestKafkaConfig(t *testing.T) {
	tests := map[string]struct {
		config         string
		errorCondition bool
		errorString    string
	}{
		"minimal": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                     topic: "test"
                    `,
			errorCondition: false,
			errorString:    "",
		},
		"disabled": {
			config: `
                     enabled: "false"
                    `,
			errorCondition: false,
			errorString:    "",
		},
		"lz4 with 0.11": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                     topic: "test"
                     compression: "lz4"
                     version: "0.11"
                    `,
			errorCondition: false,
			errorString:    "",
		},
		"lz4 with 1.0": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                     topic: "test"
                     compression: "lz4"
                     version: "1.0.0"
                    `,
			errorCondition: false,
			errorString:    "",
		},
		"Kerberos with keytab": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                     topic: "test"
                     kerberos:
                       auth_type: "keytab"
                       username: "elastic"
                       keytab: "/etc/krb5kcd/kafka.keytab"
                       config_path: "/etc/path/config"
                       service_name: "HTTP/elastic@ELASTIC"
                       realm: "ELASTIC"
                    `,
			errorCondition: false,
			errorString:    "",
		},
		"no topic": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                    `,
			errorCondition: true,
			errorString:    "setting 'topic' and/or 'topics' is required accessing config",
		},
		"no hosts": {
			config: `
                     enabled: "true"
                     topic: "test"
                    `,
			errorCondition: true,
			errorString:    "missing required field 'hosts' accessing config",
		},
		"too low timeout": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                     topic: "test"
                     timeout: 0
                    `,
			errorCondition: true,
			errorString:    "requires duration >= 1 accessing 'timeout'",
		},
		"negative keep alive": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                     topic: "test"
                     keep_alive: -1
                    `,
			errorCondition: true,
			errorString:    "requires duration >= 0 accessing 'keep_alive'",
		},
		"invalid max_message_bytes": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                     topic: "test"
                     max_message_bytes: 0
                    `,
			errorCondition: true,
			errorString:    "requires value >= 1 accessing 'max_message_bytes'",
		},
		"invalid required acks": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                     topic: "test"
                     required_acks: -2
                    `,
			errorCondition: true,
			errorString:    "requires value >= -1 accessing 'required_acks'",
		},
		"invalid broker timeout": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                     topic: "test"
                     broker_timeout: 0
                    `,
			errorCondition: true,
			errorString:    "requires duration >= 1 accessing 'broker_timeout'",
		},
		"zero retries": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                     topic: "test"
                     max_retries: 0
                    `,
			errorCondition: true,
			errorString:    "zero value accessing 'max_retries'",
		},
		"invalid channel buffer size": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                     topic: "test"
                     channel_buffer_size: 0
                    `,
			errorCondition: true,
			errorString:    "requires value >= 1 accessing 'channel_buffer_size'",
		},
		"Kerberos with invalid auth_type": {
			config: `
                     enabled: "true"
                     hosts: "localhost:9092"
                     topic: "test"
                     kerberos:
                       auth_type: "invalid_auth_type"
                       config_path: "/etc/path/config"
                       service_name: "HTTP/elastic@ELASTIC"
                       realm: "ELASTIC"
                    `,
			errorCondition: true,
			errorString:    "invalid authentication type 'invalid_auth_type' accessing 'kerberos.auth_type'",
		},
	}

	for name, tc := range tests {
		cfg, err := libconfig.NewConfigWithYAML([]byte(tc.config), "")
		require.NoErrorf(t, err, "%s: error making config from yaml: %s, %s", name, tc.config, err)
		config := DefaultConfig()
		err = cfg.Unpack(&config)
		validErr := config.Validate()

		if !tc.errorCondition && (err != nil || validErr != nil) {
			t.Fatalf("%s: unexpected error: %s", name, err)
		}
		if tc.errorCondition && err == nil && validErr == nil {
			t.Fatalf("%s: supposed to have error", name)
		}
		if tc.errorCondition {
			require.EqualErrorf(t, err, tc.errorString, "%s: errors don't match", name)
		}
		if _, err := newSaramaConfig(logp.L(), config); err != nil {
			t.Fatalf("Failure creating sarama config: %v", err)
		}

	}
}

func TestTopicSelection(t *testing.T) {
	cases := map[string]struct {
		//cfg   map[string]interface{}
		cfg   string
		event beat.Event
		want  string
	}{
		"topic configured": {
			cfg: `
                  enabled: "true"
                  hosts: "localhost:9092"
                  topic: "test"
                  `,
			event: beat.Event{
				Fields: mapstr.M{"field": "anything"},
			},
			want: "test",
		},
		"topic must keep case": {
			cfg: `
                  enabled: "true"
                  hosts: "localhost:9092"
                  topic: "Test"
                  `,
			event: beat.Event{
				Fields: mapstr.M{"field": "anything"},
			},
			want: "Test",
		},
		"topics setting": {
			cfg: `
                  enabled: "true"
                  hosts: "localhost:9092"
                  topics:
                  - topic: "test"
                  `,
			event: beat.Event{
				Fields: mapstr.M{"field": "anything"},
			},
			want: "test",
		},
		"topics setting must keep case": {
			cfg: `
                  enabled: "true"
                  hosts: "localhost:9092"
                  topics:
                  - topic: "Test"
                  `,
			event: beat.Event{
				Fields: mapstr.M{"field": "anything"},
			},
			want: "Test",
		},
		"topic uses event field": {
			cfg: `
                  enabled: "true"
                  hosts: "localhost:9092"
                  topic: "test-%{[field]}"
                  `,
			event: beat.Event{
				Fields: mapstr.M{"field": "from-event"},
			},
			want: "test-from-event",
		},
		"topic using event field must keep correct case": {
			cfg: `
                  enabled: "true"
                  hosts: "localhost:9092"
                  topic: "Test-%{[field]}"
                  `,
			event: beat.Event{
				Fields: mapstr.M{"field": "From-Event"},
			},
			want: "Test-From-Event",
		},
		"Topic should select from rule over default when matched": {
			cfg: `
                  enabled: "true"
                  hosts: "localhost:9092"
                  topics:
                  - topic: "Test-Found"
                    when.contains:
                      field: "Matched"
                  - topic: "Default"
                    default:
                  `,
			event: beat.Event{
				Fields: mapstr.M{"field": "Matched"},
			},
			want: "Test-Found",
		},
		"Topic should select default when rule not matched": {
			cfg: `
                  enabled: "true"
                  hosts: "localhost:9092"
                  topics:
                  - topic: "Topic-Found"
                    when.contains:
                      field: "Matched"
                  - topic: "Default"
                    default:
                  `,
			event: beat.Event{
				Fields: mapstr.M{"field": "Not here"},
			},
			want: "Default",
		},
		"Topic should select default from single topic: field when rule not matched and no multi key default": {
			cfg: `
                 enabled: "true"
                 hosts: "localhost:9092"
                 topic: "Default single"
                 topics:
                 - topic: "Topic-Found"
                   when.contains:
                     field: "Matched"
                 `,
			event: beat.Event{
				Fields: mapstr.M{"field": "Not here"},
			},
			want: "Default single",
		},
		"Topic should not select default from single topic: field when rule matched": {
			cfg: `
                  enabled: "true"
                  hosts: "localhost:9092"
                  topic: "Default"
                  topics:
                  - topic: "Topic-Found"
                    when.contains:
                      field: "Matched"
                  `,
			event: beat.Event{
				Fields: mapstr.M{"field": "Matched"},
			},
			want: "Topic-Found",
		},
		"Topic should select default from multikey topics: field when rule not matched and multi key has default": {
			cfg: `
                 enabled: "true"
                 hosts: "localhost:9092"
                 topic: "Default single"
                 topics:
                 - topic: "Topic-Found"
                   when.contains:
                     field: "Matched"
                 - topic: "Default"
                   default:
                 `,
			event: beat.Event{
				Fields: mapstr.M{"field": "Not here"},
			},
			want: "Default",
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := libconfig.NewConfigWithYAML([]byte(test.cfg), "")
			require.NoErrorf(t, err, "%s: error making config from yaml: %s, %s", name, test.cfg, err)
			config := DefaultConfig()
			err = cfg.Unpack(&config)

			if err != nil {
				t.Fatalf("Failed to parse configuration: %v", err)
			}

			selector, err := buildTopicSelectorFromConfig(config)

			if err != nil {
				t.Fatalf("Failed to parse configuration: %v", err)
			}

			got, err := selector.Select(&test.event)

			if err != nil {
				t.Fatalf("Failed to create topic name: %v", err)
			}

			if test.want != got {
				t.Errorf("%v: Topic name missmatch (want: %v, got: %v)", name, test.want, got)
			}
		})
	}
}
