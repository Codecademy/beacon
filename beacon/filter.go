package beacon

import (
	"github.com/pkg/errors"
	"strings"
)

// NewFilter creates a new filter which returns true if a container has all of
// the provided labels.
func NewFilter(labels map[string]string) Filter {
	if labels == nil {
		labels = map[string]string{}
	}
	return &labelFilter{
		labels: labels,
	}
}

// ParseFilter creates a filter from the provided pattern. The pattern has the
// form 'label1=value1,label2=value2,...'. The container must match all of the
// lable/value pairs. Only matching against labels is currently supported.
func ParseFilter(pattern string) (Filter, error) {
	if pattern == "" {
		return &labelFilter{}, nil
	}
	pairs := strings.Split(pattern, ",")
	labels := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) > 1 {
			labels[parts[0]] = parts[1]
		} else {
			return nil, errors.Errorf("invalid filter pattern: %s", pattern)
		}
	}
	return &labelFilter{labels: labels}, nil
}

// Filter is used to match containers againston a set of criteria.
type Filter interface {
	MatchContainer(*Container) bool
}

// Basic filter which checks that the container has all of the given label values.
type labelFilter struct {
	labels map[string]string
}

func (f *labelFilter) MatchContainer(c *Container) bool {
	for label, value1 := range f.labels {
		if value2, ok := c.Labels[label]; !ok || value1 != value2 {
			return false
		}
	}
	return true
}

// A filter that matches everything.
type allFilter struct{}

// MatchContainer returns true.
func (*allFilter) MatchContainer(*Container) bool {
	return true
}
