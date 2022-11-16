// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

/*
import (
	"bytes"
	"errors"

	"github.com/elastic/elastic-agent-libs/logp"
)

var (
	errExpectedItemsArray    = errors.New("expected items array")
	errExpectedItemObject    = errors.New("expected item response object")
	errExpectedStatusCode    = errors.New("expected item status code")
	errUnexpectedEmptyObject = errors.New("empty object")
	errExpectedObjectEnd     = errors.New("expected end of object")

	nameItems  = []byte("items")
	nameStatus = []byte("status")
	nameError  = []byte("error")
)

// bulkReadToItems reads the bulk response up to (but not including) items
func bulkReadToItems(reader *jsonReader) error {
	if err := reader.ExpectDict(); err != nil {
		return errExpectedObject
	}

	// find 'items' field in response
	for {
		kind, name, err := reader.nextFieldName()
		if err != nil {
			return err
		}

		if kind == dictEnd {
			return errExpectedItemsArray
		}

		// found items array -> continue
		if bytes.Equal(name, nameItems) {
			break
		}

		_, _ = reader.ignoreNext()
	}

	// check items field is an array
	if err := reader.ExpectArray(); err != nil {
		return errExpectedItemsArray
	}

	return nil
}

// bulkReadItemStatus reads the status and error fields from the bulk item
func bulkReadItemStatus(logger *logp.Logger, reader *jsonReader) (int, []byte, error) {
	// skip outer dictionary
	if err := reader.ExpectDict(); err != nil {
		return 0, nil, errExpectedItemObject
	}

	// find first field in outer dictionary (e.g. 'create')
	kind, _, err := reader.nextFieldName()
	if err != nil {
		logger.Errorf("Failed to parse bulk response item: %s", err)
		return 0, nil, err
	}
	if kind == dictEnd {
		err = errUnexpectedEmptyObject
		logger.Errorf("Failed to parse bulk response item: %s", err)
		return 0, nil, err
	}

	// parse actual item response code and error message
	status, msg, err := itemStatusInner(reader, logger)
	if err != nil {
		logger.Errorf("Failed to parse bulk response item: %s", err)
		return 0, nil, err
	}

	// close dictionary. Expect outer dictionary to have only one element
	kind, _, err = reader.step()
	if err != nil {
		logger.Errorf("Failed to parse bulk response item: %s", err)
		return 0, nil, err
	}
	if kind != dictEnd {
		err = errExpectedObjectEnd
		logger.Errorf("Failed to parse bulk response item: %s", err)
		return 0, nil, err
	}

	return status, msg, nil
}

func itemStatusInner(reader *jsonReader, logger *logp.Logger) (int, []byte, error) {
	if err := reader.ExpectDict(); err != nil {
		return 0, nil, errExpectedItemObject
	}

	status := -1
	var msg []byte
	for {
		kind, name, err := reader.nextFieldName()
		if err != nil {
			logger.Errorf("Failed to parse bulk response item: %s", err)
		}
		if kind == dictEnd {
			break
		}

		switch {
		case bytes.Equal(name, nameStatus): // name == "status"
			status, err = reader.nextInt()
			if err != nil {
				logger.Errorf("Failed to parse bulk response item: %s", err)
				return 0, nil, err
			}

		case bytes.Equal(name, nameError): // name == "error"
			msg, err = reader.ignoreNext() // collect raw string for "error" field
			if err != nil {
				return 0, nil, err
			}

		default: // ignore unknown fields
			_, err = reader.ignoreNext()
			if err != nil {
				return 0, nil, err
			}
		}
	}

	if status < 0 {
		return 0, nil, errExpectedStatusCode
	}
	return status, msg, nil
}
*/
