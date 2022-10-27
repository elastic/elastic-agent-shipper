// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.elastic.co/apm"

	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/beats/v7/libbeat/beat/events"
	"github.com/elastic/beats/v7/libbeat/esleg/eslegclient"
	"github.com/elastic/beats/v7/libbeat/outputs"
	"github.com/elastic/beats/v7/libbeat/outputs/outil"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/mapstr"
	"github.com/elastic/elastic-agent-libs/testing"
	"github.com/elastic/elastic-agent-libs/version"

	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
)

var (
	errPayloadTooLarge = errors.New("the bulk payload is too large for the server. Consider to adjust `http.max_content_length` parameter in Elasticsearch or `bulk_max_size` in the beat. The batch has been dropped")

	//nolint:stylecheck // Elasticsearch should be capitalized
	ErrTooOld = errors.New("Elasticsearch is too old. Please upgrade the instance. If you would like to connect to older instances set output.elasticsearch.allow_older_versions to true.")
)

// Client is an elasticsearch client.
type Client struct {
	conn eslegclient.Connection

	//index outputs.IndexSelector
	//pipeline *outil.Selector

	//observer           outputs.Observer

	log *logp.Logger
}

// ClientSettings contains the settings for a client.
type ClientSettings struct {
	eslegclient.ConnectionSettings
	Index              outputs.IndexSelector
	Pipeline           *outil.Selector
	Observer           outputs.Observer
	NonIndexableAction string
}

type bulkResultStats struct {
	acked        int // number of events ACKed by Elasticsearch
	duplicates   int // number of events failed with `create` due to ID already being indexed
	fails        int // number of failed events (can be retried)
	nonIndexable int // number of failed events (not indexable)
	tooMany      int // number of events receiving HTTP 429 Too Many Requests
}

// NewClient instantiates a new client.
func NewClient(
	s ClientSettings,
	onConnect *callbacksRegistry,
) (*Client, error) {
	conn, err := eslegclient.NewConnection(eslegclient.ConnectionSettings{
		URL:              s.URL,
		Beatname:         s.Beatname,
		Username:         s.Username,
		Password:         s.Password,
		APIKey:           s.APIKey,
		Headers:          s.Headers,
		Kerberos:         s.Kerberos,
		Observer:         s.Observer,
		Parameters:       s.Parameters,
		CompressionLevel: s.CompressionLevel,
		EscapeHTML:       s.EscapeHTML,
		Transport:        s.Transport,
	})
	if err != nil {
		return nil, err
	}

	conn.OnConnectCallback = func() error {
		globalCallbackRegistry.mutex.Lock()
		defer globalCallbackRegistry.mutex.Unlock()

		for _, callback := range globalCallbackRegistry.callbacks {
			err := callback(conn)
			if err != nil {
				return err
			}
		}

		if onConnect != nil {
			onConnect.mutex.Lock()
			defer onConnect.mutex.Unlock()

			for _, callback := range onConnect.callbacks {
				err := callback(conn)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}

	client := &Client{
		conn: *conn,
		//index:    s.Index,
		//pipeline: pipeline,
		//observer:           s.Observer,

		log: logp.NewLogger("elasticsearch"),
	}

	return client, nil
}

/*type PublishResult struct {
	retry []*messages.Event
	drop []*messages.Event
	err   error
}
*/

/*func (client *Client) Publish(ctx context.Context, events []*messages.Event) error {
	rest, err := client.publishEvents(ctx, events)

	switch {
	case err == errPayloadTooLarge:
		// report fatal error
	case len(rest) == 0:
		batch.ACK()
	default:
		batch.RetryEvents(rest)
	}
	return err
}*/

func mapstrForValue(v *messages.Value) interface{} {
	if boolVal, ok := v.GetKind().(*messages.Value_BoolValue); ok {
		return boolVal.BoolValue
	}
	if listVal, ok := v.GetKind().(*messages.Value_ListValue); ok {
		return mapstrForList(listVal.ListValue)
	}
	if nullVal, ok := v.GetKind().(*messages.Value_NullValue); ok {
		return nullVal.NullValue
	}
	if intVal, ok := v.GetKind().(*messages.Value_NumberValue); ok {
		return intVal.NumberValue
	}
	if strVal, ok := v.GetKind().(*messages.Value_StringValue); ok {
		return strVal.StringValue
	}
	if structVal, ok := v.GetKind().(*messages.Value_StructValue); ok {
		return mapstrForStruct(structVal.StructValue)
	}
	if tsVal, ok := v.GetKind().(*messages.Value_TimestampValue); ok {
		return tsVal.TimestampValue.AsTime()
	}
	return nil
}

func mapstrForList(list *messages.ListValue) []interface{} {
	results := []interface{}{}
	for _, val := range list.Values {
		results = append(results, mapstrForValue(val))
	}
	return results
}

func mapstrForStruct(proto *messages.Struct) mapstr.M {
	data := proto.GetData()
	result := mapstr.M{}
	for key, value := range data {
		result[key] = mapstrForValue(value)
	}
	return result
}

func beatsEventForProto(e *messages.Event) *beat.Event {
	return &beat.Event{
		Timestamp: e.GetTimestamp().AsTime(),
		Meta:      mapstrForStruct(e.GetMetadata()),
		Fields:    mapstrForStruct(e.GetFields()),
	}
}

// PublishEvents sends all events to elasticsearch. On error a slice with all
// events not published or confirmed to be processed by elasticsearch will be
// returned. The input slice backing memory will be reused by return the value.
func (client *Client) publishEvents(ctx context.Context, data []*messages.Event) ([]*messages.Event, error) {
	span, ctx := apm.StartSpan(ctx, "publishEvents", "output")
	defer span.End()
	begin := time.Now()

	/*st := client.observer

	if st != nil {
		st.NewBatch(len(data))
	}*/

	if len(data) == 0 {
		return nil, nil
	}

	// encode events into bulk request buffer, dropping failed elements from
	// events slice
	origCount := len(data)
	span.Context.SetLabel("events_original", origCount)

	data, bulkItems := client.bulkEncodePublishRequest(client.conn.GetVersion(), data)
	newCount := len(data)

	span.Context.SetLabel("events_encoded", newCount)
	/*if st != nil && origCount > newCount {
		st.Dropped(origCount - newCount)
	}*/
	if newCount == 0 {
		return nil, nil
	}

	status, result, sendErr := client.conn.Bulk(ctx, "", "", nil, bulkItems)

	if sendErr != nil {
		if status == http.StatusRequestEntityTooLarge {
			sendErr = errPayloadTooLarge
		}
		err := apm.CaptureError(ctx, fmt.Errorf("failed to perform any bulk index operations: %w", sendErr))
		err.Send()
		client.log.Error(err)
		return data, sendErr
	}
	pubCount := len(data)
	span.Context.SetLabel("events_published", pubCount)

	client.log.Debugf("PublishEvents: %d events have been published to elasticsearch in %v.",
		pubCount,
		time.Since(begin))

	// check response for transient errors
	var failedEvents []*messages.Event
	var stats bulkResultStats
	if status != 200 {
		failedEvents = data
		stats.fails = len(failedEvents)
	} else {
		failedEvents, _ = client.bulkCollectPublishFails(result, data)
	}

	failed := len(failedEvents)
	span.Context.SetLabel("events_failed", failed)
	/*if st := client.observer; st != nil {
		dropped := stats.nonIndexable
		duplicates := stats.duplicates
		acked := len(data) - failed - dropped - duplicates

		st.Acked(acked)
		st.Failed(failed)
		st.Dropped(dropped)
		st.Duplicate(duplicates)
		st.ErrTooMany(stats.tooMany)
	}*/

	if failed > 0 {
		if sendErr == nil {
			sendErr = eslegclient.ErrTempBulkFailure
		}
		return failedEvents, sendErr
	}
	return nil, nil
}

// bulkEncodePublishRequest encodes all bulk requests and returns slice of events
// successfully added to the list of bulk items and the list of bulk items.
func (client *Client) bulkEncodePublishRequest(version version.V, data []*messages.Event) ([]*messages.Event, []interface{}) {
	okEvents := make([]*messages.Event, len(data))
	bulkItems := []interface{}{}
	for _, event := range data {
		meta, err := client.createEventBulkMeta(version, event)
		if err != nil {
			client.log.Errorf("Failed to encode event meta data: %+v", err)
			continue
		}
		bulkItems = append(bulkItems, meta, beatsEventForProto(event))
		okEvents = append(okEvents, event)
	}
	return okEvents, bulkItems
}

func GetOpType(e *messages.Event) events.OpType {
	opStr, err := getMetaStringValue(e, events.FieldMetaOpType)
	if err != nil {
		return events.OpTypeDefault
	}

	switch opStr {
	case "create":
		return events.OpTypeCreate
	case "index":
		return events.OpTypeIndex
	case "delete":
		return events.OpTypeDelete
	}

	return events.OpTypeDefault
}

func (client *Client) createEventBulkMeta(version version.V, event *messages.Event) (interface{}, error) {
	eventType := ""
	/*if version.Major < 7 {
		eventType = defaultEventType
	}*/

	pipeline := ""
	/*pipeline, err := client.getPipeline(event)
	if err != nil {
		err := fmt.Errorf("failed to select pipeline: %v", err)
		return nil, err
	}*/

	index := "elastic-agent-shipper"
	/*index, err := client.index.Select(event)
	if err != nil {
		err := fmt.Errorf("failed to select event index: %v", err)
		return nil, err
	}*/

	id, _ := getMetaStringValue(event, events.FieldMetaID)
	opType := GetOpType(event)

	meta := eslegclient.BulkMeta{
		Index:    index,
		DocType:  eventType,
		Pipeline: pipeline,
		ID:       id,
	}

	if opType == events.OpTypeIndex {
		return eslegclient.BulkIndexAction{Index: meta}, nil
	}
	return eslegclient.BulkCreateAction{Create: meta}, nil
}

// bulkCollectPublishFails checks per item errors returning all events
// to be tried again due to error code returned for that items. If indexing an
// event failed due to some error in the event itself (e.g. does not respect mapping),
// the event will be dropped.
func (client *Client) bulkCollectPublishFails(result eslegclient.BulkResult, data []*messages.Event) ([]*messages.Event, bulkResultStats) {
	reader := newJSONReader(result)
	if err := bulkReadToItems(reader); err != nil {
		client.log.Errorf("failed to parse bulk response: %v", err.Error())
		return nil, bulkResultStats{}
	}

	count := len(data)
	failed := data[:0]
	stats := bulkResultStats{}
	for i := 0; i < count; i++ {
		status, msg, err := bulkReadItemStatus(client.log, reader)
		if err != nil {
			client.log.Error(err)
			return nil, bulkResultStats{}
		}

		if status < 300 {
			stats.acked++
			continue // ok value
		}

		if status == 409 {
			// 409 is used to indicate an event with same ID already exists if
			// `create` op_type is used.
			stats.duplicates++
			continue // ok
		}

		if status < 500 {
			if status == http.StatusTooManyRequests {
				stats.tooMany++
			} else {
				stats.nonIndexable++
				client.log.Warnf("Cannot index event %#v (status=%v): %s, dropping event!", data[i], status, msg)
				continue
			}
		}

		client.log.Debugf("Bulk item insert failed (i=%v, status=%v): %s", i, status, msg)
		stats.fails++
		failed = append(failed, data[i])
	}

	return failed, stats
}

func (client *Client) Connect() error {
	return client.conn.Connect()
}

func (client *Client) Close() error {
	return client.conn.Close()
}

func (client *Client) String() string {
	return "elasticsearch(" + client.conn.URL + ")"
}

func (client *Client) Test(d testing.Driver) {
	client.conn.Test(d)
}
