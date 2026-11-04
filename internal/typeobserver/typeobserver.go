// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package typeobserver counts the types of values it observes, and when each
// was first and last seen, for failure messages.
package typeobserver

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

type observedType struct {
	name          string
	count         int
	firstReceived time.Time
	lastReceived  time.Time
}

// Observer counts the types of values it observes, and when each was first
// and last seen. Its String method lists them, newest first. It is safe for
// concurrent use.
type Observer struct {
	mu    sync.RWMutex
	types map[string]*observedType
}

// New returns an empty Observer.
func New() *Observer {
	return &Observer{types: make(map[string]*observedType)}
}

// Observe records v, which must not be a nil interface.
func (o *Observer) Observe(v any) {
	name := reflect.TypeOf(v).String()
	now := time.Now()

	o.mu.Lock()
	defer o.mu.Unlock()
	obs, ok := o.types[name]
	if !ok {
		obs = &observedType{name: name, firstReceived: now}
		o.types[name] = obs
	}
	obs.count++
	obs.lastReceived = now
}

// sortedLastReceived returns the observed types, newest first. Caller must
// hold mu.
func (o *Observer) sortedLastReceived() []*observedType {
	values := slices.Collect(maps.Values(o.types))
	slices.SortFunc(values, func(a, b *observedType) int {
		return b.lastReceived.Compare(a.lastReceived)
	})
	return values
}

func (o *Observer) String() string {
	o.mu.RLock()
	defer o.mu.RUnlock()

	b := strings.Builder{}
	b.WriteString("observed types:")
	if len(o.types) == 0 {
		b.WriteString(" <none>")
		return b.String()
	}
	for _, obs := range o.sortedLastReceived() {
		fmt.Fprintf(&b,
			"\n  %q x%d -- first: %s ago",
			obs.name,
			obs.count,
			time.Since(obs.firstReceived),
		)
		if !obs.firstReceived.Equal(obs.lastReceived) {
			fmt.Fprintf(&b,
				", last: %s ago",
				time.Since(obs.lastReceived),
			)
		}
	}
	return b.String()
}
