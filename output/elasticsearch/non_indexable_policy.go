// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package elasticsearch

/*
const (
	dead_letter_marker_field = "deadlettered"
	drop            = "drop"
	deadLetterIndex = "dead_letter_index"
)

type DropPolicy struct{}

func (d DropPolicy) action() string {
	return drop
}

func (d DropPolicy) index() string {
	panic("drop policy doesn't have an target index")
}

type DeadLetterIndexPolicy struct {
	Index string
}

func (d DeadLetterIndexPolicy) action() string {
	return deadLetterIndex
}

func (d DeadLetterIndexPolicy) index() string {
	return d.Index
}

type nonIndexablePolicy interface {
	action() string
	index() string
}

var (
	policyFactories = map[string]policyFactory{
		drop:            newDropPolicy,
		deadLetterIndex: newDeadLetterIndexPolicy,
	}
)

func newDeadLetterIndexPolicy(config *config.C) (nonIndexablePolicy, error) {
	cfgwarn.Beta("The non_indexable_policy dead_letter_index is beta.")
	policy := DeadLetterIndexPolicy{}
	err := config.Unpack(&policy)
	if policy.index() == "" {
		return nil, fmt.Errorf("%s policy requires an `index` to be specified specified", deadLetterIndex)
	}
	return policy, err
}

func newDropPolicy(*config.C) (nonIndexablePolicy, error) {
	return defaultDropPolicy(), nil
}

func defaultPolicy() nonIndexablePolicy {
	return defaultDropPolicy()
}

func defaultDropPolicy() nonIndexablePolicy {
	return &DropPolicy{}
}

type policyFactory func(config *config.C) (nonIndexablePolicy, error)

func newNonIndexablePolicy(configNamespace *config.Namespace) (nonIndexablePolicy, error) {
	if configNamespace == nil {
		return defaultPolicy(), nil
	}

	policyType := configNamespace.Name()
	factory, ok := policyFactories[policyType]
	if !ok {
		return nil, fmt.Errorf("no such policy type: %s", policyType)
	}

	return factory(configNamespace.Config())
}
*/
