package events

import (
	"k8s.io/api/core/v1"
)

// Any returns true if any call to `f` evaluates to `true`
func Any(events []v1.Event, f func(v1.Event) bool) bool {
	for _, e := range events {
		if f(e) {
			return true
		}
	}
	return false
}

// None returns true if any call to `f` evaluates to `false`
func None(events []v1.Event, f func(v1.Event) bool) bool {
	return !Any(events, f)
}

// Select filters the given events according to the given function `f`
func Select(events []v1.Event, f func(v1.Event) bool) []v1.Event {
	filtered := []v1.Event{}
	for _, e := range events {
		if f(e) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
