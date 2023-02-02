package config

import (
	"testing"

	"github.com/elastic/elastic-agent-client/v7/pkg/client"
	"github.com/elastic/elastic-agent-client/v7/pkg/proto"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestConfig(t *testing.T) {

	testSource, err := structpb.NewStruct(map[string]interface{}{
		"type":     "elasticsearch",
		"enabled":  true,
		"hosts":    []interface{}{"localhost:9200"},
		"username": "elastic",
		"password": "changeme",
	})
	require.NoError(t, err)

	testConfig := &proto.UnitExpectedConfig{
		Source: testSource,
	}
	// check the log level as well
	logp.SetLevel(zapcore.InfoLevel)

	shipperCfg, err := ShipperConfigFromUnitConfig(client.UnitLogLevelDebug, testConfig)
	require.NoError(t, err)

	require.Equal(t, logp.GetLevel(), zapcore.DebugLevel)

	require.NotNil(t, shipperCfg.Shipper.Output.Elasticsearch)
	require.Equal(t, "elastic", shipperCfg.Shipper.Output.Elasticsearch.Username)
	require.Equal(t, "changeme", shipperCfg.Shipper.Output.Elasticsearch.Password)
	require.True(t, shipperCfg.Shipper.Output.Elasticsearch.Enabled)

	require.Nil(t, shipperCfg.Shipper.Output.Console)
	require.Nil(t, shipperCfg.Shipper.Output.Kafka)
	require.Nil(t, shipperCfg.Shipper.Output.Logstash)
}
