package kafka

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-libs/config"
    "github.com/elastic/elastic-agent-libs/logp"
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
            errorString:    "missing required field 'topic' accessing config",
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
		cfg, err := config.NewConfigWithYAML([]byte(tc.config), "")
		require.NoErrorf(t, err, "%s: error making config from yaml: %s, %s", name, tc.config, err)
		config := DefaultConfig()
		err = cfg.Unpack(&config)
		config.Validate()

        if !tc.errorCondition && err != nil {
			t.Fatalf("%s: unexpected error: %s", name, err)
		}
		if tc.errorCondition && err == nil {
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
